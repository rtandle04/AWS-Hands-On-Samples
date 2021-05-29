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

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"

	"istio.io/pkg/log"

	"github.com/prometheus/common/model"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	dashboards = []struct {
		configmap string
		name      string
		excluded  []string
	}{
		{
			"istio-grafana-configuration-dashboards-pilot-dashboard",
			"pilot-dashboard.json",
			[]string{
				"pilot_xds_push_errors",
				"pilot_total_xds_internal_errors",
				"pilot_xds_push_context_errors",
				`pilot_xds_pushes{type!~"lds|cds|rds|eds"}`,
				"pilot_xds_eds_instances",
				"_timeout",
				"_rejects",
				// In default install, we have no proxy
				"istio-proxy",
				// cAdvisor does not expose this metrics, and we don't have kubelet in kind
				"container_fs_usage_bytes",
			},
		},
		{
			"istio-grafana-configuration-dashboards-istio-mesh-dashboard",
			"istio-mesh-dashboard.json",
			[]string{
				"galley_",
				"istio_tcp_",
			},
		},
		{
			"istio-grafana-configuration-dashboards-istio-service-dashboard",
			"istio-service-dashboard.json",
			[]string{
				"istio_tcp_",
			},
		},
		{
			"istio-grafana-configuration-dashboards-istio-workload-dashboard",
			"istio-workload-dashboard.json",
			[]string{
				"istio_tcp_",
			},
		},
		{
			"istio-grafana-configuration-dashboards-istio-performance-dashboard",
			"istio-performance-dashboard.json",
			[]string{
				// TODO add these back: https://github.com/istio/istio/issues/20175
				`istio-telemetry`,
				`istio-policy`,
				// cAdvisor does not expose this metrics, and we don't have kubelet in kind
				"container_fs_usage_bytes",
			},
		},
		{
			"istio-grafana-configuration-dashboards-galley-dashboard",
			"galley-dashboard.json",
			[]string{
				// Exclude all metrics -- galley is disabled by default
				"_",
			},
		},
		{
			"istio-grafana-configuration-dashboards-mixer-dashboard",
			"mixer-dashboard.json",
			[]string{
				// Exclude all metrics -- mixer is disabled by default
				"_",
			},
		},
		{
			"istio-grafana-configuration-dashboards-citadel-dashboard",
			"citadel-dashboard.json",
			[]string{
				// Exclude all metrics -- citadel is disabled by default
				"_",
			},
		},
	}
)

func TestDashboard(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			p := prometheus.NewOrFail(ctx, ctx)
			kenv := ctx.Environment().(*kube.Environment)
			setupDashboardTest(ctx)
			waitForMetrics(ctx, p)
			for _, d := range dashboards {
				d := d
				ctx.NewSubTest(d.name).RunParallel(func(t framework.TestContext) {
					cm, err := kenv.Accessor.GetConfigMap(d.configmap, i.Settings().TelemetryNamespace)
					if err != nil {
						t.Fatalf("Failed to find dashboard %v: %v", d.configmap, err)
					}

					config, f := cm.Data[d.name]
					if !f {
						t.Fatalf("Failed to find expected dashboard: %v", d.name)
					}

					queries, err := extractQueries(config)
					if err != nil {
						t.Fatalf("Failed to extract queries: %v", err)
					}

					for _, query := range queries {
						if err := checkMetric(p, query, d.excluded); err != nil {
							t.Errorf("Check query failed: %v", err)
						}
					}
				})
			}
		})
}

var (
	// Some templates use replacement variables. Instead, replace those with wildcard
	replacer = strings.NewReplacer(
		"$dstns", ".*",
		"$dstwl", ".*",
		"$service", ".*",
		"$srcns", ".*",
		"$srcwl", ".*",
		"$namespace", ".*",
		"$workload", ".*",
		"$dstsvc", ".*",
		"$adapter", ".*",
		// Just allow all mTLS settings rather than trying to send mtls and plaintext
		`connection_security_policy="unknown"`, `connection_security_policy=~".*"`,
		`connection_security_policy="mutual_tls"`, `connection_security_policy=~".*"`,
		`connection_security_policy!="mutual_tls"`, `connection_security_policy=~".*"`,
		// Test runs in istio-system
		`destination_workload_namespace!="istio-system"`, `destination_workload_namespace=~".*"`,
		`source_workload_namespace!="istio-system"`, `source_workload_namespace=~".*"`,
	)
)

func checkMetric(p prometheus.Instance, query string, excluded []string) error {
	query = replacer.Replace(query)
	value, _, err := p.API().QueryRange(context.Background(), query, promv1.Range{
		Start: time.Now().Add(-time.Minute),
		End:   time.Now(),
		Step:  time.Second,
	})
	if err != nil {
		return fmt.Errorf("failure executing query (%s): %v", query, err)
	}
	if value == nil {
		return fmt.Errorf("returned value should not be nil for '%s'", query)
	}
	numSamples := 0
	switch v := value.(type) {
	case model.Vector:
		numSamples = v.Len()
	case model.Matrix:
		numSamples = v.Len()
	case *model.Scalar:
		numSamples = 1
	default:
		return fmt.Errorf("unknown metric value type: %T", v)
	}
	if includeQuery(query, excluded) {
		if numSamples == 0 {
			return fmt.Errorf("expected a metric value for '%s', found no samples: %#v", query, value)
		}
	} else {
		if numSamples != 0 {
			scopes.CI.Debugf("Filtered out metric '%v', but got samples: %v", query, value)
		}
	}
	return nil
}

func waitForMetrics(t framework.TestContext, instance prometheus.Instance) {
	// These are sentinel metrics that will be used to evaluate if prometheus
	// scraping has occurred and data is available via promQL.
	// We will retry these queries, but not future ones, otherwise failures will take too long
	queries := []string{
		`istio_requests_total`,
		`istio_tcp_received_bytes_total`,
	}

	for _, query := range queries {
		err := retry.UntilSuccess(func() error {
			return checkMetric(instance, query, nil)
		})
		// Do not fail here - this is just to let the metrics sync. We will fail on the test if query fails
		if err != nil {
			t.Logf("Sentinel query %v failed: %v", query, err)
		}
	}
}

const gatewayConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: echo-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
  - port:
      number: 15029
      name: tcp
      protocol: TCP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: echo
spec:
  hosts:
  - "*"
  gateways:
  - echo-gateway
  http:
  - match:
    - uri:
        exact: /echo-%s
    route:
    - destination:
        host: server
        port:
          number: 80
  tcp:
  - match:
    - port: 15029
    route:
    - destination:
        host: server
        port:
          number: 7777
`

func setupDashboardTest(t framework.TestContext) {
	ns := namespace.NewOrFail(t, t, namespace.Config{
		Prefix: "dashboard",
		Inject: true,
	})
	g.ApplyConfigOrFail(t, ns, fmt.Sprintf(gatewayConfig, ns.Name()))

	var instance echo.Instance
	echoboot.
		NewBuilderOrFail(t, t).
		With(&instance, echo.Config{
			Service:   "server",
			Pilot:     p,
			Galley:    g,
			Namespace: ns,
			Subsets:   []echo.SubsetConfig{{}},
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// We use a port > 1024 to not require root
					InstancePort: 8090,
				},
				{
					Name:         "tcp",
					Protocol:     protocol.TCP,
					InstancePort: 7777,
					ServicePort:  7777,
				},
			},
		}).
		BuildOrFail(t)

	// Send 200 http requests, 20 tcp requests across goroutines, generating a variety of error codes.
	// Spread out over 5s so rate() queries will behave correctly
	g, _ := errgroup.WithContext(context.Background())
	addr := ingr.HTTPAddress()
	tcpAddr := ingr.TCPAddress()
	ticker := time.NewTicker(time.Second * 5)
	for t := 0; t < 20; t++ {
		<-ticker.C
		g.Go(func() error {
			for i := 0; i < 10; i++ {
				_, err := ingr.Call(ingress.CallOptions{
					Host:     "server",
					Path:     fmt.Sprintf("/echo-%s?codes=418:10,520:15,200:75", ns.Name()),
					CallType: ingress.PlainText,
					Address:  addr,
				})
				if err != nil {
					// Do not fail on errors since there may be initial startup errors
					// These calls are not under tests, the dashboards are, so we can be leniant here
					log.Warnf("requests failed: %v", err)
				}
			}
			_, err := ingr.Call(ingress.CallOptions{
				Host:     "server",
				Path:     fmt.Sprintf("/echo-%s", ns.Name()),
				CallType: ingress.PlainText,
				Address:  tcpAddr,
			})
			if err != nil {
				// Do not fail on errors since there may be initial startup errors
				// These calls are not under tests, the dashboards are, so we can be leniant here
				log.Warnf("requests failed: %v", err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("ingress call failed: %v", err)
	}
}

// extractQueries pulls all prometheus queries out of a grafana dashboard
// Rather than importing the entire grafana API just for this test, do some shoddy json parsing
// Equivalent to jq command: '.panels[].targets[]?.expr'
func extractQueries(dash string) ([]string, error) {
	var queries []string
	js := map[string]interface{}{}
	if err := json.Unmarshal([]byte(dash), &js); err != nil {
		return nil, err
	}
	panels, f := js["panels"]
	if !f {
		return nil, fmt.Errorf("failed to find panels in %v", dash)
	}
	panelsList, f := panels.([]interface{})
	if !f {
		return nil, fmt.Errorf("failed to find panelsList in type %T: %v", panels, panels)
	}
	for _, p := range panelsList {
		pm := p.(map[string]interface{})
		targets, f := pm["targets"]
		if !f {
			continue
		}
		targetsList, f := targets.([]interface{})
		if !f {
			return nil, fmt.Errorf("failed to find targetsList in type %T: %v", targets, targets)
		}
		for _, t := range targetsList {
			tm := t.(map[string]interface{})
			expr, f := tm["expr"]
			if !f {
				return nil, fmt.Errorf("failed to find expr in %v", t)
			}
			queries = append(queries, expr.(string))
		}
	}
	return queries, nil
}

func includeQuery(query string, excluded []string) bool {
	for _, f := range excluded {
		if strings.Contains(query, f) {
			return false
		}
	}
	return true
}
