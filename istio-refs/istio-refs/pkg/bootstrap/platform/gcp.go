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

package platform

import (
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/compute/metadata"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/pkg/env"

	"istio.io/pkg/log"
)

const (
	GCPProject       = "gcp_project"
	GCPProjectNumber = "gcp_project_number"
	GCPCluster       = "gcp_gke_cluster_name"
	GCPLocation      = "gcp_location"
	GCEInstanceID    = "gcp_gce_instance_id"
)

var (
	gcpMetadataVar = env.RegisterStringVar("GCP_METADATA", "", "Pipe separted GCP metadata, schemed as PROJECT_ID|PROJECT_NUMBER|CLUSTER_NAME|CLUSTER_ZONE")
)

var (
	clusterNameFn = func() (string, error) {
		cn, err := metadata.InstanceAttributeValue("cluster-name")
		if err != nil {
			return "", err
		}
		return cn, nil
	}
	clusterLocationFn = func() (string, error) {
		cl, err := metadata.InstanceAttributeValue("cluster-location")
		if err == nil {
			return cl, nil
		}
		return metadata.Zone()
	}
)

type shouldFillFn func() bool
type metadataFn func() (string, error)

type gcpEnv struct {
	shouldFillMetadata shouldFillFn
	projectIDFn        metadataFn
	numericProjectIDFn metadataFn
	locationFn         metadataFn
	clusterNameFn      metadataFn
	instanceIDFn       metadataFn
}

// IsGCP returns whether or not the platform for bootstrapping is Google Cloud Platform.
func IsGCP() bool {
	if gcpMetadataVar.Get() != "" {
		// Assume this is running on GCP if GCP project env variable is set.
		return true
	}
	return metadata.OnGCE()
}

// NewGCP returns a platform environment customized for Google Cloud Platform.
// Metadata returned by the GCP Environment is taken from the GCE metadata
// service.
func NewGCP() Environment {
	return &gcpEnv{
		shouldFillMetadata: metadata.OnGCE,
		projectIDFn:        metadata.ProjectID,
		numericProjectIDFn: metadata.NumericProjectID,
		locationFn:         clusterLocationFn,
		clusterNameFn:      clusterNameFn,
		instanceIDFn:       metadata.InstanceID,
	}
}

// Metadata returns GCP environmental data, including project, cluster name, and
// location information.
func (e *gcpEnv) Metadata() map[string]string {
	md := map[string]string{}
	if e == nil {
		return md
	}
	if gcpMetadataVar.Get() == "" && !e.shouldFillMetadata() {
		return md
	}
	envPid, envNPid, envCN, envLoc := parseGCPMetadata()
	if envPid != "" {
		md[GCPProject] = envPid
	} else if pid, err := e.projectIDFn(); err == nil {
		md[GCPProject] = pid
	}
	if envNPid != "" {
		md[GCPProjectNumber] = envNPid
	} else if npid, err := e.numericProjectIDFn(); err == nil {
		md[GCPProjectNumber] = npid
	}
	if envLoc != "" {
		md[GCPLocation] = envLoc
	} else if l, err := e.locationFn(); err == nil {
		md[GCPLocation] = l
	}
	if envCN != "" {
		md[GCPCluster] = envCN
	} else if cn, err := e.clusterNameFn(); err == nil {
		md[GCPCluster] = cn
	}
	if id, err := e.instanceIDFn(); err == nil {
		md[GCEInstanceID] = id
	}
	return md
}

func parseGCPMetadata() (pid, npid, cluster, location string) {
	gcpmd := gcpMetadataVar.Get()
	log.Infof("Extract GCP metadata from env variable GCP_METADATA: %v", gcpmd)
	parts := strings.Split(gcpmd, "|")
	if len(parts) != 4 {
		return
	}
	return parts[0], parts[1], parts[2], parts[3]
}

// Converts a GCP zone into a region.
func zoneToRegion(z string) (string, error) {
	// Zones are in the form <region>-<zone_suffix>, so capture everything but the suffix.
	re := regexp.MustCompile("(.*)-.*")
	m := re.FindStringSubmatch(z)
	if len(m) != 2 {
		return "", fmt.Errorf("unable to extract region from GCP zone: %s", z)
	}
	return m[1], nil
}

// Locality returns the GCP-specific region and zone.
func (e *gcpEnv) Locality() *core.Locality {
	var l core.Locality
	if metadata.OnGCE() {
		z, zerr := metadata.Zone()
		if zerr != nil {
			log.Warnf("Error fetching GCP zone: %v", zerr)
			return &l
		}
		r, rerr := zoneToRegion(z)
		if rerr != nil {
			log.Warnf("Error fetching GCP region: %v", rerr)
			return &l
		}
		l.Region = r
		l.Zone = z
	}

	return &l
}
