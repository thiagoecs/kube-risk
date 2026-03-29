package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thiagomcp/kube-risk/internal/github"
	"github.com/thiagomcp/kube-risk/internal/k8s"
	"github.com/thiagomcp/kube-risk/internal/llm"
	"github.com/thiagomcp/kube-risk/internal/patcher"
	"github.com/thiagomcp/kube-risk/internal/rules"
)

var (
	flagPRRepo       string
	flagPathTemplate string
	flagGitHubToken  string
	flagLLMKey       string
	flagDryRun       bool
)

type workloadKey struct{ namespace, name string }

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
		`Path template to locate manifests, e.g. "manifests/{namespace}/{name}.yaml" (optional — auto-discovered if omitted)`)
	prCmd.Flags().StringVar(&flagGitHubToken, "token", "",
		"GitHub personal access token (default: $GITHUB_TOKEN)")
	prCmd.Flags().StringVar(&flagLLMKey, "llm-key", "",
		"Anthropic API key for AI-generated fixes (default: $ANTHROPIC_API_KEY). When set, unfixable findings are patched by Claude instead of opening an Issue.")
	prCmd.Flags().BoolVar(&flagDryRun, "dry-run", false,
		"Print what would be done without creating branches or PRs (no token required)")

	_ = prCmd.MarkFlagRequired("repo")
}

func runPR(cmd *cobra.Command, args []string) error {
	token := flagGitHubToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	llmKey := flagLLMKey
	if llmKey == "" {
		llmKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if token == "" && !flagDryRun {
		return fmt.Errorf("GitHub token required: set GITHUB_TOKEN or use --token (or use --dry-run to preview without a token)")
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

	findings, suppressed, err := runner.RunAll(context.Background())
	if err != nil {
		return err
	}
	if suppressed > 0 {
		fmt.Fprintf(os.Stderr, "%d finding(s) suppressed via kube-risk/suppress annotation.\n", suppressed)
	}

	// Filter to findings that have a mechanically-derivable fix.
	var fixable []rules.Finding
	for _, f := range findings {
		if f.Fix != "" {
			fixable = append(fixable, f)
		}
	}

	// Group fixable findings by workload so we open one PR per file.
	groups := make(map[workloadKey][]rules.Finding)
	var order []workloadKey // preserve deterministic output order
	for _, f := range fixable {
		key := workloadKey{f.Namespace, f.Name}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}

	// All findings per workload — used to give the PR a complete picture,
	// including findings that can't be auto-fixed.
	allFindings := make(map[workloadKey][]rules.Finding)
	for _, f := range findings {
		key := workloadKey{f.Namespace, f.Name}
		allFindings[key] = append(allFindings[key], f)
	}

	// Collect workloads that have findings but no fixable ones — get an Issue.
	fixableWorkloads := make(map[workloadKey]bool, len(groups))
	for key := range groups {
		fixableWorkloads[key] = true
	}
	unfixable := make(map[workloadKey][]rules.Finding)
	var unfixableOrder []workloadKey
	for _, f := range findings {
		key := workloadKey{f.Namespace, f.Name}
		if fixableWorkloads[key] {
			continue
		}
		if _, seen := unfixable[key]; !seen {
			unfixableOrder = append(unfixableOrder, key)
		}
		unfixable[key] = append(unfixable[key], f)
	}

	// When an LLM key is available, unfixable-only workloads get a PR with an
	// AI-generated patch instead of a GitHub Issue. Merge them into the PR flow.
	if llmKey != "" {
		for _, key := range unfixableOrder {
			if _, seen := groups[key]; !seen {
				order = append(order, key)
				groups[key] = nil // no mechanical fixes, LLM handles everything
			}
		}
		unfixableOrder = nil
		unfixable = make(map[workloadKey][]rules.Finding)
		if len(order) > 0 {
			fmt.Fprintf(os.Stderr, "ANTHROPIC_API_KEY detected — AI-generated fixes will be applied for unfixable findings.\n")
		}
	}

	gh := github.New(token, flagPRRepo)

	if len(order) == 0 {
		fmt.Println("No findings — nothing to open PRs for.")
		return nil
	}

	if len(fixable) == 0 && llmKey == "" {
		fmt.Println("No fixable findings — nothing to open PRs for.")
		if len(unfixableOrder) > 0 {
			fmt.Fprintf(os.Stderr, "\nOpening issue(s) for findings with no auto-fix...\n")
			for _, key := range unfixableOrder {
				issueTitle := fmt.Sprintf("kube-risk: %s/%s needs manual attention", key.namespace, key.name)
				fmt.Fprintf(os.Stderr, "  Opening issue for %s/%s ... ", key.namespace, key.name)
				if existing, err := gh.FindOpenIssue(issueTitle); err == nil && existing != "" {
					fmt.Fprintf(os.Stderr, "already open\n")
					fmt.Printf("%s\n", existing)
					continue
				}
				url, err := gh.CreateIssue(issueTitle, buildIssueBody(key.namespace, key.name, unfixable[key]), []string{"kube-risk"})
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
					continue
				}
				fmt.Fprintf(os.Stderr, "done\n")
				fmt.Printf("%s\n", url)
			}
		}
		return nil
	}

	// Build namespace/name → file path map.
	// Use --path-template if provided; otherwise auto-discover from the repo.
	// In dry-run mode with no token, skip discovery and show generic paths.
	pathMap, err := buildPathMap(gh, groups, token)
	if err != nil {
		return err
	}

	if flagDryRun {
		return runDryRun(groups, order, pathMap, unfixableOrder, unfixable)
	}

	defaultBranch, err := gh.DefaultBranch()
	if err != nil {
		return fmt.Errorf("fetching default branch: %w", err)
	}

	prCount := 0
	for _, key := range order {
		filePath, ok := pathMap[key.namespace+"/"+key.name]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Skipping %s/%s — manifest not found in repo\n", key.namespace, key.name)
			continue
		}
		branchName := fmt.Sprintf("kube-risk/fix-%s-%s", key.namespace, key.name)
		fmt.Fprintf(os.Stderr, "  Opening PR for %s/%s ... ", key.namespace, key.name)
		if existing, err := gh.FindOpenPR(branchName); err == nil && existing != "" {
			fmt.Fprintf(os.Stderr, "already open\n")
			fmt.Printf("%s\n", existing)
			prCount++
			continue
		}
		url, err := openWorkloadPR(gh, key.namespace, key.name, groups[key], allFindings[key], llmKey, defaultBranch, filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "done\n")
		fmt.Printf("%s\n", url)
		prCount++
	}

	fmt.Fprintf(os.Stderr, "\n%d PR(s) opened.\n", prCount)

	issueCount := 0
	if len(unfixableOrder) > 0 {
		fmt.Fprintf(os.Stderr, "\nOpening issue(s) for findings with no auto-fix...\n")
		for _, key := range unfixableOrder {
			issueTitle := fmt.Sprintf("kube-risk: %s/%s needs manual attention", key.namespace, key.name)
			fmt.Fprintf(os.Stderr, "  Opening issue for %s/%s ... ", key.namespace, key.name)
			if existing, err := gh.FindOpenIssue(issueTitle); err == nil && existing != "" {
				fmt.Fprintf(os.Stderr, "already open\n")
				fmt.Printf("%s\n", existing)
				issueCount++
				continue
			}
			url, err := gh.CreateIssue(issueTitle, buildIssueBody(key.namespace, key.name, unfixable[key]), []string{"kube-risk"})
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "done\n")
			fmt.Printf("%s\n", url)
			issueCount++
		}
		fmt.Fprintf(os.Stderr, "%d issue(s) opened.\n", issueCount)
	}
	return nil
}

// buildPathMap returns a map of "namespace/name" → file path.
// If --path-template is set, it builds the map from the template.
// Otherwise it auto-discovers manifests by scanning the repo.
// If no token is available (dry-run without credentials), returns an empty map.
func buildPathMap(gh *github.Client, groups map[workloadKey][]rules.Finding, token string) (map[string]string, error) {
	if flagPathTemplate != "" {
		m := make(map[string]string, len(groups))
		for key := range groups {
			m[key.namespace+"/"+key.name] = resolvePath(flagPathTemplate, key.namespace, key.name)
		}
		return m, nil
	}
	if token == "" {
		// No token — can't scan repo. Dry-run will show workloads without file paths.
		return make(map[string]string), nil
	}
	fmt.Fprintf(os.Stderr, "No --path-template provided — scanning repo for manifests...\n")
	m, err := gh.DiscoverManifests()
	if err != nil {
		return nil, fmt.Errorf("discovering manifests: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d manifest(s) in repo.\n", len(m))
	return m, nil
}

// runDryRun prints what kube-risk pr would do without touching GitHub.
func runDryRun(groups map[workloadKey][]rules.Finding, order []workloadKey, pathMap map[string]string, unfixableOrder []workloadKey, unfixable map[workloadKey][]rules.Finding) error {
	fmt.Println("DRY RUN — no branches or PRs will be created")
	fmt.Println()
	for _, key := range order {
		filePath, ok := pathMap[key.namespace+"/"+key.name]
		findings := groups[key]
		fmt.Printf("  Would open PR for %s/%s  (branch: kube-risk/fix-%s-%s)\n",
			key.namespace, key.name, key.namespace, key.name)

		for _, f := range findings {
			path := filePath
			if !ok {
				path = fmt.Sprintf("<manifest for %s/%s>", key.namespace, key.name)
			}
			switch f.Rule {
			case "single-replica":
				fmt.Printf("    patch  %s  →  spec.replicas: 2\n", path)
			case "unsafe-rollout":
				fmt.Printf("    patch  %s  →  spec.strategy.rollingUpdate.maxUnavailable: 1\n", path)
			case "missing-pdb":
				if ok {
					fmt.Printf("    create %s\n", pdbFilePathFromResolved(filePath, key.name))
				} else {
					fmt.Printf("    create <pdb for %s/%s>\n", key.namespace, key.name)
				}
			}
		}
		fmt.Println()
	}
	fmt.Printf("%d PR(s) would be opened.\n", len(order))
	if len(unfixableOrder) > 0 {
		fmt.Printf("%d issue(s) would be opened for findings with no auto-fix:\n", len(unfixableOrder))
		for _, key := range unfixableOrder {
			ruleNames := make([]string, 0, len(unfixable[key]))
			seen := make(map[string]bool)
			for _, f := range unfixable[key] {
				if !seen[f.Rule] {
					ruleNames = append(ruleNames, f.Rule)
					seen[f.Rule] = true
				}
			}
			fmt.Printf("  %s/%s (%s)   %s\n", key.namespace, key.name, unfixable[key][0].Kind, strings.Join(ruleNames, ", "))
		}
	}
	return nil
}

// openWorkloadPR creates a branch, commits the patched file (and PDB file if
// needed), then opens a pull request. Returns the PR URL.
// llmKey is optional — when non-empty, unfixable findings are patched by Claude.
func openWorkloadPR(gh *github.Client, namespace, name string, fixableFindings, allFindings []rules.Finding, llmKey, defaultBranch, filePath string) (string, error) {
	branchName := fmt.Sprintf("kube-risk/fix-%s-%s", namespace, name)

	repoFile, err := gh.GetFile(filePath)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", filePath, err)
	}

	// Step 1: apply mechanical YAML patches.
	patched, err := patcher.PatchFile(repoFile.Content, fixableFindings)
	if err != nil {
		return "", fmt.Errorf("patching %s: %w", filePath, err)
	}
	mainFileChanged := string(patched) != string(repoFile.Content)

	// Step 2: apply LLM-generated fixes for unfixable findings when key is set.
	var llmFixedFindings []rules.Finding
	if llmKey != "" {
		var unfixable []rules.Finding
		for _, f := range allFindings {
			if f.Fix == "" {
				unfixable = append(unfixable, f)
			}
		}
		if len(unfixable) > 0 {
			llmPatched, err := llm.Suggest(llmKey, patched, unfixable)
			if err != nil {
				// LLM failure is non-fatal — log and continue with mechanical fixes only.
				fmt.Fprintf(os.Stderr, "\n    [LLM] warning: %v — skipping AI fixes\n", err)
			} else if string(llmPatched) != string(patched) {
				patched = llmPatched
				mainFileChanged = true
				llmFixedFindings = unfixable
			}
		}
	}

	// Collect missing-pdb findings — each needs a new PDB file.
	var pdbFinding *rules.Finding
	for i := range fixableFindings {
		if fixableFindings[i].Rule == "missing-pdb" {
			pdbFinding = &fixableFindings[i]
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
		pdbPath := pdbFilePathFromResolved(filePath, name)
		pdbContent := []byte(patcher.ExtractPDBYAML(pdbFinding.Fix))
		if err := gh.PutFile(pdbPath, branchName, "fix: add PDB for "+name+" in "+namespace, pdbContent, ""); err != nil {
			return "", fmt.Errorf("committing PDB %s: %w", pdbPath, err)
		}
	}

	prURL, err := gh.CreatePR(
		fmt.Sprintf("fix(kube-risk): %s in %s", name, namespace),
		buildPRBody(allFindings, llmFixedFindings),
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

// pdbFilePathFromResolved returns the path for a new PDB manifest in the same
// directory as the resolved workload file path, named "{name}-pdb.yaml".
func pdbFilePathFromResolved(resolvedPath, name string) string {
	if i := strings.LastIndex(resolvedPath, "/"); i >= 0 {
		return resolvedPath[:i] + "/" + name + "-pdb.yaml"
	}
	return name + "-pdb.yaml"
}

// buildIssueBody produces the GitHub issue body for a workload with unfixable findings.
func buildIssueBody(namespace, name string, findings []rules.Finding) string {
	var sb strings.Builder
	sb.WriteString("## kube-risk findings — manual attention required\n\n")
	sb.WriteString(fmt.Sprintf("**Workload:** `%s/%s` (%s)\n\n", namespace, name, findings[0].Kind))
	sb.WriteString("The following findings were detected but cannot be fixed automatically ")
	sb.WriteString("because the correct fix requires app-specific knowledge:\n\n")
	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("### `%s` — %s severity\n\n", f.Rule, f.Severity))
		sb.WriteString(f.Message + "\n\n")
	}
	sb.WriteString("---\n_Opened automatically by [kube-risk](https://github.com/thiagoecs/kube-risk)._\n")
	return sb.String()
}

// buildPRBody produces the pull request description listing all findings.
// llmFixed is the subset of findings that were patched by the LLM (may be nil).
// Mechanically-fixed findings show the applied change; LLM-fixed ones are marked
// for review; remaining unfixable ones are listed for manual attention.
func buildPRBody(findings []rules.Finding, llmFixed []rules.Finding) string {
	llmRules := make(map[string]bool, len(llmFixed))
	for _, f := range llmFixed {
		llmRules[f.Rule] = true
	}

	var fixed, aiFixed, manual []rules.Finding
	for _, f := range findings {
		switch {
		case f.Fix != "":
			fixed = append(fixed, f)
		case llmRules[f.Rule]:
			aiFixed = append(aiFixed, f)
		default:
			manual = append(manual, f)
		}
	}

	var sb strings.Builder
	sb.WriteString("## kube-risk findings\n\n")
	sb.WriteString("This PR was generated by [kube-risk](https://github.com/thiagoecs/kube-risk).\n\n")

	if len(fixed) > 0 {
		sb.WriteString("### ✅ Fixed in this PR\n\n")
		for _, f := range fixed {
			sb.WriteString(fmt.Sprintf("**`%s`** — %s severity\n\n", f.Rule, f.Severity))
			sb.WriteString(f.Message + "\n\n")
			sb.WriteString("<details><summary>Change applied</summary>\n\n```\n")
			sb.WriteString(f.Fix)
			sb.WriteString("\n```\n\n</details>\n\n")
		}
	}

	if len(aiFixed) > 0 {
		sb.WriteString("### 🤖 AI-generated fix — please review before merging\n\n")
		sb.WriteString("These fixes were generated by Claude based on the workload spec. ")
		sb.WriteString("They are best-effort — verify the probe paths, ports, and timing values match your app before merging:\n\n")
		for _, f := range aiFixed {
			sb.WriteString(fmt.Sprintf("**`%s`** — %s severity\n\n", f.Rule, f.Severity))
			sb.WriteString(f.Message + "\n\n")
		}
	}

	if len(manual) > 0 {
		sb.WriteString("### ⚠️ Requires manual attention\n\n")
		sb.WriteString("These findings were detected but cannot be fixed automatically — ")
		sb.WriteString("the correct fix requires app-specific knowledge:\n\n")
		for _, f := range manual {
			sb.WriteString(fmt.Sprintf("**`%s`** — %s severity\n\n", f.Rule, f.Severity))
			sb.WriteString(f.Message + "\n\n")
		}
	}

	sb.WriteString("---\n_Opened automatically by [kube-risk](https://github.com/thiagoecs/kube-risk)._\n")
	return sb.String()
}
