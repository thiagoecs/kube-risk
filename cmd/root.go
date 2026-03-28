package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kube-risk",
	Short: "Analyze Kubernetes clusters for upgrade and resilience risks",
	Long: `kube-risk connects to your Kubernetes cluster and runs a set of rules
to identify workloads that could cause downtime during cluster upgrades or
under normal operational stress.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
