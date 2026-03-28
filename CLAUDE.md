# kube-risk — Claude Code guide

## What this project is

A Go CLI tool that connects to a Kubernetes cluster via kubeconfig, runs a set of rules, and prints a risk report explaining what could cause downtime during cluster upgrades (AKS/EKS). The core product value is the *explanation* — not just flagging a problem, but telling the operator why it matters and what to do.

## Build & run

```bash
# Build
go build -o kube-risk.exe .

# Run against the local test cluster
go run . analyze -n risky-apps

# Run against all namespaces
go run . analyze

# Point at a specific kubeconfig
go run . analyze --kubeconfig ~/.kube/my-cluster.yaml
```

After any code change, always verify with `go build ./...` before considering the task done.

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
- `production` — 1 workload: `single-in-prod`, created during V2 testing to verify the namespace boost (+2 score for prod namespaces). Will appear in all-namespace scans.

## Project layout

```
main.go                          — entry point, calls cmd.Execute()
cmd/
  root.go                        — root Cobra command
  analyze.go                     — "kube-risk analyze" (flags: --kubeconfig, -n)
internal/
  k8s/client.go                  — builds kubernetes.Interface from kubeconfig
  rules/
    types.go                     — Finding (includes Score int), Severity types
    runner.go                    — RunAll() → runs rules → calls ApplyScores()
    scoring.go                   — base scores per rule:severity, namespace boost, ApplyScores()
    single_replica.go            — Deployment/StatefulSet with replicas <= 1 (HIGH, score 9)
    missing_pdb.go               — no matching PodDisruptionBudget (MEDIUM, score 5)
    missing_readiness_probe.go   — container missing readinessProbe (HIGH, score 7)
    unsafe_rollout.go            — maxUnavailable >= 50% of replicas (MEDIUM, score 4)
    risky_statefulset.go         — OnDelete (HIGH, score 8) or Parallel (MEDIUM, score 4)
  report/printer.go              — findings list → workload summary → "Fix this first"
test-cluster/
  broken-workloads.yaml          — intentionally misconfigured workloads for testing
```

## Adding a new rule

1. Create `internal/rules/your_rule_name.go` with a function matching this signature:
   ```go
   func CheckYourRule(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error)
   ```
2. Register it in `internal/rules/runner.go` inside the `allRules` slice.
3. Add a base score entry to the `baseScores` map in `internal/rules/scoring.go` using the key `"rule-id:SEVERITY"`. Without an entry it falls back to 7/4/2 by severity.
4. Add an entry to `whyItMatters()` in `internal/report/printer.go` for the "Fix this first" rationale.
5. Add a test workload to `test-cluster/broken-workloads.yaml` that triggers it.
6. Run `go build ./...` and `go run . analyze -n risky-apps` to confirm.

## Key conventions

- **Finding.Message must explain the risk in plain language.** Don't just say "missing PDB" — say what will happen during an upgrade and why it matters. This is the core product differentiator.
- **One finding per workload per rule.** If multiple containers in a pod are missing a readiness probe, report the workload once (break after the first hit).
- **Severity is intentional:** HIGH = can cause immediate downtime during a drain. MEDIUM = increases blast radius or reduces observability.
- **Exit code 1 on any HIGH finding** — this makes the tool usable in CI pipelines.
- **No mocks in rule code** — rules take a real `kubernetes.Interface`. If you need to test a rule in isolation, use `fake.NewSimpleClientset()` from `k8s.io/client-go/kubernetes/fake`.

## Roadmap (don't implement ahead of the current phase)

| Version | Focus |
|---------|-------|
| V1 | CLI + 5 rules + report — **done** |
| V2 | Risk scoring, prioritization, environment awareness — **done** |
| V3 | Specific fix recommendations per finding |
| V4 | GitHub/GitLab PR generation with YAML fixes |
| V5 | LLM layer that reads repo and adapts to team conventions |
