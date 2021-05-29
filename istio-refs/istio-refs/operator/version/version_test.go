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

package version

import (
	"io/ioutil"
	"testing"

	"gopkg.in/yaml.v2"

	"istio.io/istio/operator/pkg/version"
)

const (
	operatorVersionsMapFilePath = "../data/versions.yaml"
)

func TestVersions(t *testing.T) {
	b, err := ioutil.ReadFile(operatorVersionsMapFilePath)
	if err != nil {
		t.Fatal(err)
	}
	var vs []version.CompatibilityMapping
	if err := yaml.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}

	for _, v := range vs {
		if OperatorBinaryGoVersion.Equal(v.OperatorVersion) {
			t.Logf("Found operator version %s in %s file.", OperatorBinaryGoVersion.String(), operatorVersionsMapFilePath)
			return
		}
	}

}
