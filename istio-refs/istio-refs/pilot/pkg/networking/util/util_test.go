// Copyright 2018 Istio Authors
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

package util

import (
	"reflect"
	"testing"
	"time"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	"gopkg.in/d4l3k/messagediff.v1"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	proto2 "istio.io/istio/pkg/proto"
)

func TestConvertAddressToCidr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want *core.CidrRange
	}{
		{
			"return nil when the address is empty",
			"",
			nil,
		},
		{
			"success case with no PrefixLen",
			"1.2.3.4",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 32,
				},
			},
		},
		{
			"success case with PrefixLen",
			"1.2.3.4/16",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 16,
				},
			},
		},
		{
			"ipv6",
			"2001:db8::",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 128,
				},
			},
		},
		{
			"ipv6 with prefix",
			"2001:db8::/64",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 64,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertAddressToCidr(tt.addr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertAddressToCidr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertLocality(t *testing.T) {
	tests := []struct {
		name     string
		locality string
		want     *core.Locality
		reverse  string
	}{
		{
			name:     "nil locality",
			locality: "",
			want:     &core.Locality{},
		},
		{
			name:     "locality with only region",
			locality: "region",
			want: &core.Locality{
				Region: "region",
			},
		},
		{
			name:     "locality with region and zone",
			locality: "region/zone",
			want: &core.Locality{
				Region: "region",
				Zone:   "zone",
			},
		},
		{
			name:     "locality with region zone and subzone",
			locality: "region/zone/subzone",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
		},
		{
			name:     "locality with region zone subzone and rack",
			locality: "region/zone/subzone/rack",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
			reverse: "region/zone/subzone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertLocality(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality %#v, but got %#v", tt.want, got)
			}
			// Verify we can reverse the conversion back to the original input
			reverse := LocalityToString(got)
			if tt.reverse != "" {
				// Special case, reverse lookup is different than original input
				if tt.reverse != reverse {
					t.Errorf("Expected locality string %s, got %v", tt.reverse, reverse)
				}
			} else if tt.locality != reverse {
				t.Errorf("Expected locality string %s, got %v", tt.locality, reverse)
			}
		})
	}
}

func TestLocalityMatch(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		rule     string
		match    bool
	}{
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1/*",
			match: true,
		},
		{
			name: "wildcard not matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone2/*",
			match: false,
		},
		{
			name: "region matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1",
			match: true,
		},
		{
			name: "region and zone matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "zubzone wildcard matching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "subzone mismatching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1/subzone2",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := LocalityMatch(tt.locality, tt.rule)
			if match != tt.match {
				t.Errorf("Expected matching result %v, but got %v", tt.match, match)
			}
		})
	}
}

func TestIsLocalityEmpty(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		want     bool
	}{
		{
			"non empty locality",
			&core.Locality{
				Region: "region",
			},
			false,
		},
		{
			"empty locality",
			&core.Locality{
				Region: "",
			},
			true,
		},
		{
			"nil locality",
			nil,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLocalityEmpty(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality empty result %#v, but got %#v", tt.want, got)
			}
		})
	}
}

func TestBuildConfigInfoMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   model.ConfigMeta
		want *core.Metadata
	}{
		{
			"destination-rule",
			model.ConfigMeta{
				Group:     "networking.istio.io",
				Version:   "v1alpha3",
				Name:      "svcA",
				Namespace: "default",
				Domain:    "svc.cluster.local",
				Type:      "destination-rule",
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1alpha3/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := BuildConfigInfoMetadata(v.in)
			if diff, equal := messagediff.PrettyDiff(got, v.want); !equal {
				tt.Errorf("BuildConfigInfoMetadata(%v) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, got, v.want, diff)
			}
		})
	}
}

func TestAddSubsetToMetadata(t *testing.T) {
	cases := []struct {
		name   string
		in     *core.Metadata
		subset string
		want   *core.Metadata
	}{
		{
			"simple subset",
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1alpha3/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
			"test-subset",
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1alpha3/namespaces/default/destination-rule/svcA",
								},
							},
							"subset": {
								Kind: &structpb.Value_StringValue{
									StringValue: "test-subset",
								},
							},
						},
					},
				},
			},
		},
		{
			"no metadata",
			&core.Metadata{},
			"test-subset",
			&core.Metadata{},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := AddSubsetToMetadata(v.in, v.subset)
			if diff, equal := messagediff.PrettyDiff(got, v.want); !equal {
				tt.Errorf("AddSubsetToMetadata(%v, %s) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, v.subset, got, v.want, diff)
			}
		})
	}
}

func TestCloneCluster(t *testing.T) {
	cluster := buildFakeCluster()
	clone := CloneCluster(cluster)
	cluster.LoadAssignment.Endpoints[0].LoadBalancingWeight.Value = 10
	cluster.LoadAssignment.Endpoints[0].Priority = 8
	cluster.LoadAssignment.Endpoints[0].LbEndpoints = nil

	if clone.LoadAssignment.Endpoints[0].LoadBalancingWeight.GetValue() == 10 {
		t.Errorf("LoadBalancingWeight mutated")
	}
	if clone.LoadAssignment.Endpoints[0].Priority == 8 {
		t.Errorf("Priority mutated")
	}
	if clone.LoadAssignment.Endpoints[0].LbEndpoints == nil {
		t.Errorf("LbEndpoints mutated")
	}
}

func buildFakeCluster() *v2.Cluster {
	return &v2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &v2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LbEndpoints: []*endpoint.LbEndpoint{},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
					LbEndpoints: []*endpoint.LbEndpoint{},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
			},
		},
	}
}

func TestIsHTTPFilterChain(t *testing.T) {
	httpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.HTTPConnectionManager,
			},
		},
	}

	tcpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.TCPProxy,
			},
		},
	}

	if !IsHTTPFilterChain(httpFilterChain) {
		t.Errorf("http Filter chain not detected properly")
	}

	if IsHTTPFilterChain(tcpFilterChain) {
		t.Errorf("tcp filter chain detected as http filter chain")
	}
}

func TestMergeAnyWithStruct(t *testing.T) {
	inHCM := &http_conn.HttpConnectionManager{
		CodecType:  http_conn.HttpConnectionManager_HTTP1,
		StatPrefix: "123",
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: "filter1",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: &any.Any{},
				},
			},
		},
		ServerName:        "scooby",
		XffNumTrustedHops: 2,
	}
	inAny := MessageToAny(inHCM)

	// listener.go sets this to 0
	newTimeout := ptypes.DurationProto(5 * time.Minute)
	userHCM := &http_conn.HttpConnectionManager{
		AddUserAgent:      proto2.BoolTrue,
		IdleTimeout:       newTimeout,
		StreamIdleTimeout: newTimeout,
		UseRemoteAddress:  proto2.BoolTrue,
		XffNumTrustedHops: 5,
		ServerName:        "foobar",
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: "some filter",
			},
		},
	}

	expectedHCM := proto.Clone(inHCM).(*http_conn.HttpConnectionManager)
	expectedHCM.AddUserAgent = userHCM.AddUserAgent
	expectedHCM.IdleTimeout = userHCM.IdleTimeout
	expectedHCM.StreamIdleTimeout = userHCM.StreamIdleTimeout
	expectedHCM.UseRemoteAddress = userHCM.UseRemoteAddress
	expectedHCM.XffNumTrustedHops = userHCM.XffNumTrustedHops
	expectedHCM.HttpFilters = append(expectedHCM.HttpFilters, userHCM.HttpFilters...)
	expectedHCM.ServerName = userHCM.ServerName

	pbStruct := MessageToStruct(userHCM)

	outAny, err := MergeAnyWithStruct(inAny, pbStruct)
	if err != nil {
		t.Errorf("Failed to merge: %v", err)
	}

	outHCM := http_conn.HttpConnectionManager{}
	if err = ptypes.UnmarshalAny(outAny, &outHCM); err != nil {
		t.Errorf("Failed to unmarshall outAny to outHCM: %v", err)
	}

	if !reflect.DeepEqual(expectedHCM, &outHCM) {
		t.Errorf("Merged HCM does not match the expected output")
	}
}

func TestIsAllowAnyOutbound(t *testing.T) {
	tests := []struct {
		name   string
		node   *model.Proxy
		result bool
	}{
		{
			name:   "NilSidecarScope",
			node:   &model.Proxy{},
			result: false,
		},
		{
			name: "NilOutboundTrafficPolicy",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{},
			},
			result: false,
		},
		{
			name: "OutboundTrafficPolicyRegistryOnly",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
					},
				},
			},
			result: false,
		},
		{
			name: "OutboundTrafficPolicyAllowAny",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
					},
				},
			},
			result: true,
		},
	}
	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			out := IsAllowAnyOutbound(tests[i].node)
			if out != tests[i].result {
				t.Errorf("Expected %t but got %t for test case: %v\n", tests[i].result, out, tests[i].node)
			}
		})
	}
}

func TestBuildStatPrefix(t *testing.T) {
	tests := []struct {
		name        string
		statPattern string
		host        string
		subsetName  string
		port        *model.Port
		attributes  model.ServiceAttributes
		want        string
	}{
		{
			"Service only pattern",
			"%SERVICE%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default",
		},
		{
			"Service only pattern from different namespace",
			"%SERVICE%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1",
		},
		{
			"Service with port pattern from different namespace",
			"%SERVICE%.%SERVICE_PORT%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1.7443",
		},
		{
			"Service from non k8s registry",
			"%SERVICE%.%SERVICE_PORT%",
			"reviews.hostname.consul",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Consul),
				Name:            "foo",
				Namespace:       "bar",
			},
			"reviews.hostname.consul.7443",
		},
		{
			"Service FQDN only pattern",
			"%SERVICE_FQDN%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local",
		},
		{
			"Service With Port pattern",
			"%SERVICE%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_7443",
		},
		{
			"Service With Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc",
		},
		{
			"Service With Port and Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc_7443",
		},
		{
			"Service FQDN With Port pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_7443",
		},
		{
			"Service FQDN With Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc",
		},
		{
			"Service FQDN With Port and Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Empty Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%.%SUBSET_NAME%.%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.v1.grpc-svc_7443",
		},
		{
			"Service FQDN With Unknown Pattern",
			"%SERVICE_FQDN%.%DUMMY%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.Kubernetes),
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.%DUMMY%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildStatPrefix(tt.statPattern, tt.host, tt.subsetName, tt.port, tt.attributes)
			if got != tt.want {
				t.Errorf("Expected alt statname %s, but got %s", tt.want, got)
			}
		})
	}
}

func TestApplyToCommonTLSContext(t *testing.T) {
	testCases := []struct {
		name       string
		sdsUdsPath string
		node       *model.Proxy
		result     *envoyauth.CommonTlsContext
	}{
		{
			name:       "MTLSStrict using SDS",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					SdsEnabled: true,
				},
			},
			result: &envoyauth.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*envoyauth.SdsSecretConfig{
					{
						Name: "default",
						SdsConfig: &core.ConfigSource{
							InitialFetchTimeout: features.InitialFetchTimeout,
							ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
								ApiConfigSource: &core.ApiConfigSource{
									ApiType: core.ApiConfigSource_GRPC,
									GrpcServices: []*core.GrpcService{
										{
											TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
												EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
											},
										},
									},
								},
							},
						},
					},
				},
				ValidationContextType: &envoyauth.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &envoyauth.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &envoyauth.CertificateValidationContext{VerifySubjectAltName: []string{} /*subjectAltNames*/},
						ValidationContextSdsSecretConfig: &envoyauth.SdsSecretConfig{
							Name: "ROOTCA",
							SdsConfig: &core.ConfigSource{
								InitialFetchTimeout: features.InitialFetchTimeout,
								ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
									ApiConfigSource: &core.ApiConfigSource{
										ApiType: core.ApiConfigSource_GRPC,
										GrpcServices: []*core.GrpcService{
											{
												TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
													EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "ISTIO_MUTUAL SDS without node meta",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			result: &envoyauth.CommonTlsContext{
				TlsCertificates: []*envoyauth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/cert-chain.pem",
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/key.pem",
							},
						},
					},
				},
				ValidationContextType: &envoyauth.CommonTlsContext_ValidationContext{
					ValidationContext: &envoyauth.CertificateValidationContext{
						TrustedCa: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/root-cert.pem",
							},
						},
					},
				},
			},
		},
		{
			name:       "ISTIO_MUTUAL with custom cert paths from proxy node metadata",
			sdsUdsPath: "/tmp/sdsuds.sock",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					TLSServerCertChain: "/custom/path/to/cert-chain.pem",
					TLSServerKey:       "/custom-key.pem",
					TLSServerRootCert:  "/custom/path/to/root.pem",
				},
			},
			result: &envoyauth.CommonTlsContext{
				TlsCertificates: []*envoyauth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/custom/path/to/cert-chain.pem",
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/custom-key.pem",
							},
						},
					},
				},
				ValidationContextType: &envoyauth.CommonTlsContext_ValidationContext{
					ValidationContext: &envoyauth.CertificateValidationContext{
						TrustedCa: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/custom/path/to/root.pem",
							},
						},
					},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tlsContext := &envoyauth.CommonTlsContext{}
			ApplyToCommonTLSContext(tlsContext, test.node.Metadata, test.sdsUdsPath, []string{})

			if !reflect.DeepEqual(tlsContext, test.result) {
				t.Errorf("got() = %v, want %v", spew.Sdump(tlsContext), spew.Sdump(test.result))
			}
		})
	}
}

func TestBuildAddress(t *testing.T) {
	testCases := []struct {
		name     string
		addr     string
		port     uint32
		expected *core.Address
	}{
		{
			name: "ipv4",
			addr: "172.10.10.1",
			port: 8080,
			expected: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "172.10.10.1",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
		},
		{
			name: "ipv6",
			addr: "fe80::10e7:52ff:fecd:198b",
			port: 8080,
			expected: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "fe80::10e7:52ff:fecd:198b",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
		},
		{
			name: "uds",
			addr: "/var/run/test/socket",
			port: 0,
			expected: &core.Address{
				Address: &core.Address_Pipe{
					Pipe: &core.Pipe{
						Path: "/var/run/test/socket",
					},
				},
			},
		},
		{
			name: "uds with unix prefix",
			addr: "unix:///var/run/test/socket",
			port: 0,
			expected: &core.Address{
				Address: &core.Address_Pipe{
					Pipe: &core.Pipe{
						Path: "/var/run/test/socket",
					},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			addr := BuildAddress(test.addr, test.port)
			if !reflect.DeepEqual(addr, test.expected) {
				t.Errorf("expected add %v, but got %v", test.expected, addr)
			}
		})
	}
}
