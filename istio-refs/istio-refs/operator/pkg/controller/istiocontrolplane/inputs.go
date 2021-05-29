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

package istiocontrolplane

import (
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
)

var (
	componentDependencies = helmreconciler.ComponentNameToListMap{
		name.IstioBaseComponentName: {
			name.PilotComponentName,
			name.PolicyComponentName,
			name.TelemetryComponentName,
			name.GalleyComponentName,
			name.CitadelComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
			name.CNIComponentName,
		},
	}

	installTree      = make(helmreconciler.ComponentTree)
	dependencyWaitCh = make(helmreconciler.DependencyWaitCh)
)

func init() {
	buildInstallTree()
	for _, parent := range componentDependencies {
		for _, child := range parent {
			dependencyWaitCh[child] = make(chan struct{}, 1)
		}
	}

}

// IstioRenderingInput is a RenderingInput specific to an v1alpha1 IstioOperator instance.
type IstioRenderingInput struct {
	instance *v1alpha1.IstioOperator
	crPath   string
}

// NewIstioRenderingInput creates a new IstioRenderingInput for the specified instance.
func NewIstioRenderingInput(instance *v1alpha1.IstioOperator) *IstioRenderingInput {
	return &IstioRenderingInput{instance: instance}
}

// GetCRPath returns the path of IstioOperator CR.
func (i *IstioRenderingInput) GetCRPath() string {
	return i.crPath
}

func (i *IstioRenderingInput) GetInputConfig() interface{} {
	return i.instance
}

func (i *IstioRenderingInput) GetTargetNamespace() string {
	if i.instance.Spec.MeshConfig == nil {
		return ""
	}
	return i.instance.Namespace
}

// GetProcessingOrder returns the order in which the rendered charts should be processed.
func (i *IstioRenderingInput) GetProcessingOrder(m helmreconciler.ChartManifestsMap) (helmreconciler.ComponentNameToListMap, helmreconciler.DependencyWaitCh) {
	componentNameList := make([]name.ComponentName, 0)
	dependencyWaitCh := make(helmreconciler.DependencyWaitCh)
	for c := range m {
		cn := name.ComponentName(c)
		if cn == name.IstioBaseComponentName {
			continue
		}
		componentNameList = append(componentNameList, cn)
		dependencyWaitCh[cn] = make(chan struct{}, 1)
	}
	componentDependencies := helmreconciler.ComponentNameToListMap{
		name.IstioBaseComponentName: componentNameList,
	}
	return componentDependencies, dependencyWaitCh
}

func buildInstallTree() {
	// Starting with root, recursively insert each first level child into each node.
	helmreconciler.InsertChildrenRecursive(name.IstioBaseComponentName, installTree, componentDependencies)
}
