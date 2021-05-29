// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package virtualservice

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// DestinationHostAnalyzer checks the destination hosts associated with each virtual service
type DestinationHostAnalyzer struct{}

var _ analysis.Analyzer = &DestinationHostAnalyzer{}

type hostAndSubset struct {
	host   resource.FullName
	subset string
}

// Metadata implements Analyzer
func (a *DestinationHostAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.DestinationHostAnalyzer",
		Description: "Checks the destination hosts associated with each virtual service",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Serviceentries.Name(),
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

// Analyze implements Analyzer
func (a *DestinationHostAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts := initServiceEntryHostMap(ctx)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		a.analyzeVirtualService(r, ctx, serviceEntryHosts)
		return true
	})
}

func (a *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Instance, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {

	vs := r.Message.(*v1alpha3.VirtualService)

	for _, d := range getRouteDestinations(vs) {
		s := getDestinationHost(r.Metadata.FullName.Namespace, d.GetHost(), serviceEntryHosts)
		if s == nil {
			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewReferencedResourceNotFound(r, "host", d.GetHost()))
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}
}

func getDestinationHost(sourceNs resource.Namespace, host string, serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) *v1alpha3.ServiceEntry {
	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := util.NewScopedFqdn(string(sourceNs), sourceNs, host)
	if s, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		return s
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, sourceNs, host)
	if s, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		return s
	}

	// Now check wildcard matches, namespace scoped or all namespaces
	// (This more expensive checking left for last)
	// Assumes the wildcard entries are correctly formatted ("*<dns suffix>")
	for seHostScopedFqdn, s := range serviceEntryHosts {
		scope, seHost := seHostScopedFqdn.GetScopeAndFqdn()

		// Skip over non-wildcard entries
		if !strings.HasPrefix(seHost, util.Wildcard) {
			continue
		}

		// Skip over entries not visible to the current virtual service namespace
		if scope != util.ExportToAllNamespaces && scope != string(sourceNs) {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, util.Wildcard)
		hostWithoutWildCard := strings.TrimPrefix(host, util.Wildcard)

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			return s
		}
	}

	return nil
}

func initServiceEntryHostMap(ctx analysis.Context) map[util.ScopedFqdn]*v1alpha3.ServiceEntry {
	result := make(map[util.ScopedFqdn]*v1alpha3.ServiceEntry)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), func(r *resource.Instance) bool {
		s := r.Message.(*v1alpha3.ServiceEntry)
		hostsNamespaceScope := string(r.Metadata.FullName.Namespace)
		if util.IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = util.ExportToAllNamespaces
		}
		for _, h := range s.GetHosts() {
			result[util.NewScopedFqdn(hostsNamespaceScope, r.Metadata.FullName.Namespace, h)] = s
		}
		return true

	})

	// converts k8s service to servcieEntry since destinationHost
	// validation is performed against serviceEntry
	ctx.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		s := r.Message.(*corev1.ServiceSpec)
		var se *v1alpha3.ServiceEntry
		hostsNamespaceScope := string(r.Metadata.FullName.Namespace)
		var ports []*v1alpha3.Port
		for _, p := range s.Ports {
			ports = append(ports, &v1alpha3.Port{
				Number:   uint32(p.Port),
				Name:     p.Name,
				Protocol: string(p.Protocol),
			})
		}
		host := util.ConvertHostToFQDN(r.Metadata.FullName.Namespace, r.Metadata.FullName.Name.String())
		se = &v1alpha3.ServiceEntry{
			Hosts: []string{host},
			Ports: ports,
		}
		result[util.NewScopedFqdn(hostsNamespaceScope, r.Metadata.FullName.Namespace, r.Metadata.FullName.Name.String())] = se
		return true

	})
	return result
}

func checkServiceEntryPorts(ctx analysis.Context, r *resource.Instance, d *v1alpha3.Destination, s *v1alpha3.ServiceEntry) {
	if d.GetPort() == nil {
		// If destination port isn't specified, it's only a problem if the service being referenced exposes multiple ports.
		if len(s.GetPorts()) > 1 {
			var portNumbers []int
			for _, p := range s.GetPorts() {
				portNumbers = append(portNumbers, int(p.GetNumber()))
			}
			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewVirtualServiceDestinationPortSelectorRequired(r, d.GetHost(), portNumbers))
			return
		}

		// Otherwise, it's not needed and we're done here.
		return
	}

	foundPort := false
	for _, p := range s.GetPorts() {
		if d.GetPort().GetNumber() == p.GetNumber() {
			foundPort = true
			break
		}
	}
	if !foundPort {
		ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewReferencedResourceNotFound(r, "host:port", fmt.Sprintf("%s:%d", d.GetHost(), d.GetPort().GetNumber())))
	}
}
