package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thiagomcp/kube-risk/internal/github"
	"github.com/thiagomcp/kube-risk/internal/k8s"
	"github.com/thiagomcp/kube-risk/internal/patcher"
	"github.com/thiagomcp/kube-risk/internal/rules"
)

var (
	flagPRRepo       string
	flagPathTemplate string
	flagGitHubToken  string
)

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Open GitHub pull requests with YAML fixes for findings",
	Long: `Runs the same analysis as 'analyze', then for each fixable finding
opens a GitHub pull request against the target repo with the corrected YAML.

Fixable rules:
  • single-replica  — sets spec.replicas to 2
  • unsafe-rollout  — sets spec.strategy.rollingUpdate.maxUnavailable to 1
  • missing-pdb     — creates a PodDisruptionBudget manifest in the same directory

One PR is opened per workload that has at least one fixable finding.

The GitHub token is read from --token or the GITHUB_TOKEN environment variable.`,
	RunE: runPR,
}

func init() {
	rootCmd.AddCommand(prCmd)

	prCmd.Flags().StringVar(&flagKubeconfig, "kubeconfig", "",
		"Path to kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	prCmd.Flags().StringVarP(&flagNamespace, "namespace", "n", "",
		"Namespace to analyze (default: all namespaces)")
	prCmd.Flags().StringVarP(&flagEnvironment, "environment", "e", "production",
		`Environment type: "production" (all rules) or "development" (config quality rules only)`)
	prCmd.Flags().StringVar(&flagPRRepo, "repo", "",
		`GitHub repository containing the manifests, e.g. "owner/repo" (required)`)
	prCmd.Flags().StringVar(&flagPathTemplate, "path-template", "",
		`Path template to locate manifests, e.g. "manifests/{namespace}/{name}.yaml" (required)`)
	prCmd.Flags().StringVar(&flagGitHubToken, "token", "",
		"GitHub personal access token (default: $GITHUB_TOKEN)")

	_ = prCmd.MarkFlagRequired("repo")
	_ = prCmd.MarkFlagRequired("path-template")
}

func runPR(cmd *cobra.Command, args []string) error {
	token := flagGitHubToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("GitHub token required: set GITHUB_TOKEN or use --token")
	}

	if flagEnvironment != "production" && flagEnvironment != "development" {
		return fmt.Errorf("--environment must be %q or %q, got %q",
			"production", "development", flagEnvironment)
	}

	client, err := k8s.NewClient(flagKubeconfig)
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	runner := &rules.Runner{
		Client:      client,
		Namespace:   flagNamespace,
		Environment: flagEnvironment,
	}

	fmt.Fprintf(os.Stderr, "Analyzing cluster")
	if flagNamespace != "" {
		fmt.Fprintf(os.Stderr, " (namespace: %s)", flagNamespace)
	} else {
		fmt.Fprintf(os.Stderr, " (all namespaces)")
	}
	fmt.Fprintf(os.Stderr, " [%s]\n", flagEnvironment)

	findings, err := runner.RunAll(context.Background())
	if err != nil {
		return err
	}

	// Filter to findings that have a mechanically-derivable fix.
	var fixable []rules.Finding
	for _, f := range findings {
		if f.Fix != "" {
			fixable = append(fixable, f)
		}
	}

	if len(fixable) == 0 {
		fmt.Println("No fixable findings — nothing to open PRs for.")
		return nil
	}

	// Group fixable findings by workload so we open one PR per file.
	type workloadKey struct{ namespace, name string }
	groups := make(map[workloadKey][]rules.Finding)
	var order []workloadKey // preserve deterministic output order
	for _, f := range fixable {
		key := workloadKey{f.Namespace, f.Name}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}

	gh := github.New(token, flagPRRepo)

	defaultBranch, err := gh.DefaultBranch()
	if err != nil {
		return fmt.Errorf("fetching default branch: %w", err)
	}

	prCount := 0
	for _, key := range order {
		fmt.Fprintf(os.Stderr, "  Opening PR for %s/%s ... ", key.namespace, key.name)
		url, err := openWorkloadPR(gh, key.namespace, key.name, groups[key], defaultBranch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "done\n")
		fmt.Printf("%s\n", url)
		prCount++
	}

	fmt.Fprintf(os.Stderr, "\n%d PR(s) opened.\n", prCount)
	return nil
}

// openWorkloadPR creates a branch, commits the patched file (and PDB file if
// needed), then opens a pull request. Returns the PR URL.
func openWorkloadPR(gh *github.Client, namespace, name string, findings []rules.Finding, defaultBranch string) (string, error) {
	filePath := resolvePath(flagPathTemplate, namespace, name)
	branchName := fmt.Sprintf("kube-risk/fix-%s-%s", namespace, name)

	repoFile, err := gh.GetFile(filePath)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", filePath, err)
	}

	// Apply YAML patches for single-replica and unsafe-rollout.
	patched, err := patcher.PatchFile(repoFile.Content, findings)
	if err != nil {
		return "", fmt.Errorf("patching %s: %w", filePath, err)
	}
	mainFileChanged := string(patched) != string(repoFile.Content)

	// Collect missing-pdb findings — each needs a new PDB file.
	var pdbFinding *rules.Finding
	for i := range findings {
		if findings[i].Rule == "missing-pdb" {
			pdbFinding = &findings[i]
			break
		}
	}

	if !mainFileChanged && pdbFinding == nil {
		return "", fmt.Errorf("no changes to apply")
	}

	if err := gh.CreateBranch(branchName, defaultBranch); err != nil {
		return "", fmt.Errorf("creating branch %q: %w", branchName, err)
	}

	commitMsg := fmt.Sprintf("fix: kube-risk findings for %s in %s", name, namespace)

	if mainFileChanged {
		if err := gh.PutFile(filePath, branchName, commitMsg, patched, repoFile.SHA); err != nil {
			return "", fmt.Errorf("committing %s: %w", filePath, err)
		}
	}

	if pdbFinding != nil {
		pdbPath := pdbFilePath(flagPathTemplate, namespace, name)
		pdbContent := []byte(patcher.ExtractPDBYAML(pdbFinding.Fix))
		if err := gh.PutFile(pdbPath, branchName, "fix: add PDB for "+name+" in "+namespace, pdbContent, ""); err != nil {
			return "", fmt.Errorf("committing PDB %s: %w", pdbPath, err)
		}
	}

	prURL, err := gh.CreatePR(
		fmt.Sprintf("fix(kube-risk): %s in %s", name, namespace),
		buildPRBody(findings),
		branchName,
		defaultBranch,
	)
	if err != nil {
		return "", fmt.Errorf("creating PR: %w", err)
	}
	return prURL, nil
}

// resolvePath substitutes {namespace} and {name} in the path template.
func resolvePath(template, namespace, name string) string {
	s := strings.ReplaceAll(template, "{namespace}", namespace)
	return strings.ReplaceAll(s, "{name}", name)
}

// pdbFilePath returns the path for a new PDB manifest in the same directory
// as the workload file, named "{name}-pdb.yaml".
func pdbFilePath(template, namespace, name string) string {
	resolved := resolvePath(template, namespace, name)
	if i := strings.LastIndex(resolved, "/"); i >= 0 {
		return resolved[:i] + "/" + name + "-pdb.yaml"
	}
	return name + "-pdb.yaml"
}

// buildPRBody produces the pull request description listing all findings being fixed.
func buildPRBody(findings []rules.Finding) string {
	var sb strings.Builder
	sb.WriteString("## kube-risk findings\n\n")
	sb.WriteString("This PR was generated by [kube-risk](https://github.com/thiagoecs/kube-risk) ")
	sb.WriteString("to address the following upgrade risks:\n\n")

	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("### `%s` — %s severity\n\n", f.Rule, f.Severity))
		sb.WriteString(f.Message + "\n\n")
		if f.Fix != "" {
			sb.WriteString("<details><summary>Fix applied</summary>\n\n```\n")
			sb.WriteString(f.Fix)
			sb.WriteString("\n```\n\n</details>\n\n")
		}
	}

	sb.WriteString("---\n_Opened automatically by [kube-risk](https://github.com/thiagoecs/kube-risk)._\n")
	return sb.String()
}
