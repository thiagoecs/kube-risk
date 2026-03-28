package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thiagomcp/kube-risk/internal/k8s"
	"github.com/thiagomcp/kube-risk/internal/report"
	"github.com/thiagomcp/kube-risk/internal/rules"
)

var (
	flagKubeconfig string
	flagNamespace  string
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze the cluster for upgrade and resilience risks",
	Long: `Connects to your Kubernetes cluster and runs a set of rules that catch
workload configurations that can cause downtime during cluster upgrades or
under normal operational pressure.

Rules checked:
  • single-replica          — single point of failure (HIGH)
  • missing-pdb             — no PodDisruptionBudget (MEDIUM)
  • missing-readiness-probe — traffic routed before app is ready (HIGH)
  • unsafe-rollout          — too many pods unavailable during updates (MEDIUM)
  • risky-statefulset       — OnDelete strategy or Parallel pod management (HIGH/MEDIUM)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := k8s.NewClient(flagKubeconfig)
		if err != nil {
			return fmt.Errorf("connecting to cluster: %w", err)
		}

		runner := &rules.Runner{
			Client:    client,
			Namespace: flagNamespace,
		}

		fmt.Fprintf(os.Stderr, "Analyzing cluster")
		if flagNamespace != "" {
			fmt.Fprintf(os.Stderr, " (namespace: %s)", flagNamespace)
		} else {
			fmt.Fprintf(os.Stderr, " (all namespaces)")
		}
		fmt.Fprintln(os.Stderr, "...")

		findings, err := runner.RunAll(context.Background())
		if err != nil {
			return err
		}

		report.Print(os.Stdout, findings)

		// Exit code 1 if any HIGH findings — useful in CI pipelines
		for _, f := range findings {
			if f.Severity == rules.SeverityHigh {
				os.Exit(1)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVar(&flagKubeconfig, "kubeconfig", "",
		"Path to kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	analyzeCmd.Flags().StringVarP(&flagNamespace, "namespace", "n", "",
		"Namespace to analyze (default: all namespaces)")
}
