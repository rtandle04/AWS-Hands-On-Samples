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
	"flag"

	"github.com/spf13/cobra"

	binversion "istio.io/istio/operator/version"
	"istio.io/pkg/version"
)

const (
	SetFlagHelpStr = `Override an IstioOperator value, e.g. to choose a profile
(--set profile=demo), enable or disable components (--set components.policy.enabled=true), or override Istio
settings (--set values.grafana.enabled=true). See documentation for more info:
https://istio.io/docs/reference/config/istio.operator.v1alpha12.pb/#IstioControlPlaneSpec`
	skipConfirmationFlagHelpStr = `skipConfirmation determines whether the user is prompted for confirmation.
If set to true, the user is not prompted and a Yes response is assumed in all cases.`
	filenameFlagHelpStr = `Path to file containing IstioOperator custom resource
This flag can be specified multiple times to overlay multiple files. Multiple files are overlaid in left to right order.`
)

type rootArgs struct {
	// logToStdErr controls whether logs are sent to stderr.
	logToStdErr bool
	// Dry run performs all steps except actually applying the manifests or creating output dirs/files.
	dryRun bool
	// Verbose controls whether additional debug output is displayed and logged.
	verbose bool
}

func addFlags(cmd *cobra.Command, rootArgs *rootArgs) {
	cmd.PersistentFlags().BoolVarP(&rootArgs.logToStdErr, "logtostderr", "",
		false, "Send logs to stderr.")
	cmd.PersistentFlags().BoolVarP(&rootArgs.dryRun, "dry-run", "",
		false, "Console/log output only, make no changes.")
	cmd.PersistentFlags().BoolVarP(&rootArgs.verbose, "verbose", "",
		false, "Verbose output.")
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "mesh",
		Short:        "Command line Istio install utility.",
		SilenceUsage: true,
		Long: "This command uses the Istio operator code to generate templates, query configurations and perform " +
			"utility operations.",
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.AddCommand(ManifestCmd())
	rootCmd.AddCommand(ProfileCmd())
	rootCmd.AddCommand(OperatorCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(UpgradeCmd())

	version.Info.Version = binversion.OperatorVersionString

	return rootCmd
}
