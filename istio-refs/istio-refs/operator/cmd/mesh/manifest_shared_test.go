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

package mesh

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// Golden output files add a lot of noise to pull requests. Use a unique suffix so
// we can hide them by default. This should match one of the `linuguist-generated=true`
// lines in istio.io/istio/.gitattributes.
const (
	goldenFileSuffixHideChangesInReview = ".golden.yaml"
	goldenFileSuffixShowChangesInReview = ".golden-show-in-gh-pull-request.yaml"
)

var (
	repoRootDir string
	testDataDir string
)

func init() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	repoRootDir = filepath.Join(wd, "../..")
}

func runCommand(command string) (string, error) {
	var out bytes.Buffer
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOutput(&out)

	if err := rootCmd.Execute(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}
