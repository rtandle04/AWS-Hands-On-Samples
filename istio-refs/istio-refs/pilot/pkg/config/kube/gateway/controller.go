// Copyright 2020 Istio Authors
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

package gateway

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	svc "sigs.k8s.io/service-apis/api/v1alpha1"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/pkg/ledger"

	"istio.io/istio/pilot/pkg/model"
)

var (
	vsType      = collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	gatewayType = collections.IstioNetworkingV1Alpha3Gateways.Resource()
)

type controller struct {
	client kubernetes.Interface
	cache  model.ConfigStoreCache
}

func (c *controller) GetLedger() ledger.Ledger {
	return c.cache.GetLedger()
}

func (c *controller) SetLedger(l ledger.Ledger) error {
	return c.cache.SetLedger(l)
}

func (c *controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.IstioNetworkingV1Alpha3Virtualservices,
		collections.IstioNetworkingV1Alpha3Gateways,
	)
}

func (c controller) Get(typ resource.GroupVersionKind, name, namespace string) *model.Config {
	panic("implement me")
}

var _ = svc.HTTPRoute{}
var _ = svc.GatewayClass{}

func (c controller) List(typ resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	if typ != gatewayType.GroupVersionKind() && typ != vsType.GroupVersionKind() {
		return nil, errUnsupportedOp
	}

	gatewayClass, err := c.cache.List(collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind(), namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type GatewayClass: %v", err)
	}
	gateway, err := c.cache.List(collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind(), namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type Gateway: %v", err)
	}
	httpRoute, err := c.cache.List(collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(), namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type HTTPRoute: %v", err)
	}
	tcpRoute, err := c.cache.List(collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind(), namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type TcpRoute: %v", err)
	}
	trafficSplit, err := c.cache.List(collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind(), namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type TrafficSplit: %v", err)
	}

	input := &KubernetesResources{
		GatewayClass: gatewayClass,
		Gateway:      gateway,
		HTTPRoute:    httpRoute,
		TCPRoute:     tcpRoute,
		TrafficSplit: trafficSplit,
	}
	output := convertResources(input)

	switch typ {
	case gatewayType.GroupVersionKind():
		return output.Gateway, nil
	case vsType.GroupVersionKind():
		return output.VirtualService, nil
	}
	return nil, errUnsupportedOp
}

var (
	errUnsupportedOp = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")
)

func (c controller) Create(config model.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Update(config model.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Delete(typ resource.GroupVersionKind, name, namespace string) error {
	return errUnsupportedOp
}

func (c controller) Version() string {
	return c.cache.Version()
}

func (c controller) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return c.cache.GetResourceAtVersion(version, key)
}

func (c controller) RegisterEventHandler(typ resource.GroupVersionKind, handler func(model.Config, model.Config, model.Event)) {
	c.cache.RegisterEventHandler(typ, func(prev, cur model.Config, event model.Event) {
		handler(prev, cur, event)
	})
}

func (c controller) Run(stop <-chan struct{}) {
}

func (c controller) HasSynced() bool {
	return c.cache.HasSynced()
}

func NewController(client kubernetes.Interface, c model.ConfigStoreCache) model.ConfigStoreCache {
	return &controller{client, c}
}
