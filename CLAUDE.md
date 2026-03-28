# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

A Go CLI tool that connects to a Kubernetes cluster via kubeconfig, runs a set of rules, and prints a risk report explaining what could cause downtime during cluster upgrades (AKS/EKS). The core product value is the *explanation* — not just flagging a problem, but telling the operator why it matters and what to do.

## Build & run

```bash
# Build
go build -o kube-risk.exe .

# Verify all packages compile (run after every change)
go build ./...

# Run against the local test cluster (production mode — default)
go run . analyze -n risky-apps

# Run in development mode (skips single-replica and missing-pdb)
go run . analyze -n risky-apps -e development

# Run against all namespaces
go run . analyze

# Point at a specific kubeconfig
go run . analyze --kubeconfig ~/.kube/my-cluster.yaml
```

## Tests

No automated tests yet. When adding them, use `fake.NewSimpleClientset()` from `k8s.io/client-go/kubernetes/fake` to construct an in-memory Kubernetes client — do not mock at the rule function level.

```bash
go test ./...                        # run all tests
go test ./internal/rules/...         # run rule tests only
go test ./internal/rules/... -run TestCheckSingleReplica  # run a single test
```

## Test cluster (kind)

```bash
# kind binary lives in the project root (not on PATH)
./kind.exe create cluster --name kube-risk-test
./kind.exe delete cluster --name kube-risk-test

# Deploy / refresh broken workloads
kubectl apply -f test-cluster/broken-workloads.yaml --context kind-kube-risk-test

# Running context
kind-kube-risk-test   # kubectl context name
risky-apps            # test namespace
```

The test cluster has two test namespaces:
- `risky-apps` — 5 workloads: `no-resilience`, `bad-rollout`, `on-delete-db`, `parallel-statefulset` (all intentionally broken), and `well-configured` (should always produce zero findings).
- `production` — 1 workload: `single-in-prod`, created to verify the namespace score boost (+2 for prod namespaces). Will appear in all-namespace scans.

GitHub CLI (`gh`) is installed but not on PATH. Full path: `C:/Program Files/GitHub CLI/gh.exe`.

## Architecture

The analysis pipeline is linear:

```
cmd/analyze.go
  → rules.Runner.RunAll()         — filters rules by environment, runs each in parallel-safe serial loop
    → each Check*() function      — queries Kubernetes API, returns []Finding
  → rules.ApplyScores()           — adds numeric score (1–10) to each finding based on rule+severity+namespace
  → report.Print()                — sorts, renders findings list, workload summary, and "Fix this first"
```

Key data type: `rules.Finding` — everything flows through this struct. Fields: `Namespace`, `Name`, `Kind`, `Rule`, `Severity`, `Score`, `Message`, `Fix`.

`Score` is computed after all rules run (not inside the rule itself) so that `ApplyScores` can apply cross-cutting concerns like namespace environment boost without coupling that logic to individual rules.

`Fix` is a copy-pasteable string (kubectl command or YAML). It is only set when the correct fix can be derived mechanically from the workload spec. Rules that require app-specific knowledge (`missing-readiness-probe`, `risky-statefulset`) leave it empty intentionally.

## Project layout

```
main.go                          — entry point, calls cmd.Execute()
cmd/
  root.go                        — root Cobra command
  analyze.go                     — "kube-risk analyze" (flags: --kubeconfig, -n, -e)
internal/
  k8s/client.go                  — builds kubernetes.Interface from kubeconfig
  rules/
    types.go                     — Finding, Severity types
    runner.go                    — RunAll(), devSkipRules, calls ApplyScores()
    scoring.go                   — baseScores map, namespaceBoost(), ApplyScores()
    single_replica.go            — replicas <= 1 (HIGH, score 9) [prod only]
    missing_pdb.go               — no matching PodDisruptionBudget (MEDIUM, score 5) [prod only]
    missing_readiness_probe.go   — container missing readinessProbe (HIGH, score 7)
    unsafe_rollout.go            — maxUnavailable >= 50% of replicas (MEDIUM, score 4)
    risky_statefulset.go         — OnDelete (HIGH, score 8) or Parallel (MEDIUM, score 4)
  report/printer.go              — findings list → workload summary → "Fix this first"
test-cluster/
  broken-workloads.yaml          — intentionally misconfigured workloads for testing
```

## Adding a new rule

1. Create `internal/rules/your_rule_name.go`:
   ```go
   func CheckYourRule(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error)
   ```
2. Register it in `internal/rules/runner.go` inside the `allRules` slice.
3. Add a base score entry to `baseScores` in `internal/rules/scoring.go` using key `"rule-id:SEVERITY"`. Falls back to 7/4/2 by severity if absent.
4. Add an entry to `whyItMatters()` in `internal/report/printer.go` for the "Fix this first" rationale.
5. Only set `Finding.Fix` if the correct fix is unambiguous and derivable from the workload spec. Leave empty if it requires app-specific knowledge.
6. Add a test workload to `test-cluster/broken-workloads.yaml` that triggers it.
7. If the rule should be skipped in development mode, add its name to `devSkipRules` in `runner.go`.
8. Run `go build ./...` and `go run . analyze -n risky-apps` to confirm.

## Key conventions

- **Finding.Message must explain the risk in plain language.** Don't just say "missing PDB" — say what will happen during an upgrade and why it matters. This is the core product differentiator.
- **One finding per workload per rule.** If multiple containers in a pod are missing a readiness probe, report the workload once (break after the first hit).
- **Severity is intentional:** HIGH = can cause immediate downtime during a drain. MEDIUM = increases blast radius or reduces observability.
- **`--environment` / `-e`** — `production` (default, all rules) or `development` (skips `single-replica` and `missing-pdb`, no namespace boost). The skipped rules are intentional in dev — flagging them trains operators to ignore the tool.
- **Exit code 1 on any HIGH finding** — makes the tool usable as a CI gate.

## Roadmap (don't implement ahead of the current phase)

| Version | Focus |
|---------|-------|
| V1 | CLI + 5 rules + report — **done** |
| V2 | Risk scoring, prioritization, environment awareness — **done** |
| V3 | Specific fix recommendations per finding — **done** |
| V4 | GitHub/GitLab PR generation with YAML fixes |
| V5 | LLM layer that reads repo and adapts to team conventions |
