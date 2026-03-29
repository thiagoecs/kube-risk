# kube-risk

**kube-risk** scans your Kubernetes cluster for workload configurations that cause downtime during node upgrades — and automatically opens pull requests to fix them.

Most teams running on Kubernetes think they're cloud native. But single replicas, missing PodDisruptionBudgets, and broken rollout strategies mean their apps are still pets, not cattle. kube-risk closes that gap.

---

## How it works

```
Cluster (live state) ──→ kube-risk finds misconfigurations
                              ↓
                     patches your YAML files
                              ↓
                     opens a GitHub PR per workload
```

kube-risk reads the live cluster state via kubeconfig, explains *why* each finding matters during an upgrade, and for fixable issues creates a branch and PR against your manifest repo with the corrected YAML.

---

## Rules

| Rule | Severity | What it catches | Environment |
|------|----------|-----------------|-------------|
| `single-replica` | HIGH | Deployments/StatefulSets with 1 replica — guaranteed downtime during node drain | production only |
| `missing-readiness-probe` | HIGH | Containers without a readiness probe — traffic hits the pod before the app is ready | all |
| `risky-statefulset` | HIGH/MEDIUM | `OnDelete` update strategy or `Parallel` pod management | all |
| `missing-pdb` | MEDIUM | No PodDisruptionBudget — Kubernetes can evict all pods simultaneously | production only |
| `unsafe-rollout` | MEDIUM | `maxUnavailable` ≥ 50% of replicas — too much capacity offline during updates | all |

---

## Installation

**Download a pre-built binary:**

```bash
curl -sSL https://github.com/thiagoecs/kube-risk/releases/download/v0.4.0/kube-risk-linux-amd64 \
  -o /usr/local/bin/kube-risk
chmod +x /usr/local/bin/kube-risk
```

**Build from source:**

```bash
go install github.com/thiagomcp/kube-risk@latest
```

---

## Usage

### Analyze your cluster

```bash
# Scan all namespaces (uses current kubeconfig context)
kube-risk analyze

# Scan a specific namespace
kube-risk analyze -n production

# Development mode — skips single-replica and missing-pdb (intentional in dev)
kube-risk analyze -n staging -e development
```

Example output:

```
────────────────────────────────────────────────────────────────────────
  KUBE-RISK REPORT (production)   3 findings  [HIGH: 2  MEDIUM: 1  LOW: 0]
────────────────────────────────────────────────────────────────────────

[1] 🔴 HIGH    production/frontend (Deployment)   score: 9/10
    Rule: single-replica
    Deployment "frontend" has only 1 replica. During a node drain
    (e.g. cluster upgrade), this pod will be evicted and the app will be
    unavailable until the new pod starts. Set replicas >= 2 to ensure
    continuity.

    ┌─ Suggested fix ────────────────────────────────────────────────
    │  kubectl scale deployment frontend -n production --replicas=2
    └───────────────────────────────────────────────────────────────────
```

Exit code is `1` if any HIGH findings are found — making kube-risk usable as a CI gate.

### Open fix PRs automatically

```bash
# Preview what would be changed (no token required)
kube-risk pr \
  --repo owner/repo \
  --path-template "manifests/{namespace}/{name}.yaml" \
  -n production \
  --dry-run

# Open real PRs (reads GITHUB_TOKEN from env)
kube-risk pr \
  --repo owner/repo \
  --path-template "manifests/{namespace}/{name}.yaml" \
  -n production
```

One PR is opened per workload with fixable findings. The PR description explains every finding — including non-fixable ones that require manual attention.

**Fixable automatically:** `single-replica`, `unsafe-rollout`, `missing-pdb`

**Require manual fix:** `missing-readiness-probe`, `risky-statefulset` — these depend on app-specific knowledge (health check port, ordering guarantees). LLM-assisted fixes are planned for V5.

---

## GitHub Action

The easiest way to run kube-risk is as a scheduled GitHub Action against your cluster.

Copy [`examples/kube-risk-action.yml`](examples/kube-risk-action.yml) to `.github/workflows/kube-risk.yml` in your manifest repo, then add two secrets:

| Secret | Description |
|--------|-------------|
| `KUBECONFIG` | Contents of your kubeconfig file pointing at the cluster to scan |
| `GITHUB_TOKEN` | Provided automatically by GitHub — no setup needed |

```yaml
- uses: thiagoecs/kube-risk@v0.4.0
  with:
    kubeconfig: ${{ secrets.KUBECONFIG }}
    github-token: ${{ secrets.GITHUB_TOKEN }}
    repo: ${{ github.repository }}
    path-template: manifests/{namespace}/{name}.yaml
    namespace: production
    environment: production
```

The action downloads the pre-built binary — no Go setup required on the runner.

---

## Roadmap

The goal is for applications running on Kubernetes to be truly cloud native — infrastructure treated as cattle, not pets. Any node should be able to die at any time without the app caring.

| Version | Focus |
|---------|-------|
| V1 | CLI + 5 rules + risk report — **done** |
| V2 | Risk scoring, prioritization, environment awareness — **done** |
| V3 | Specific fix recommendations per finding — **done** |
| V4 | GitHub PR generation with YAML fixes + GitHub Action — **done** |
| V5 | LLM layer that adapts fixes to team conventions (readiness probes, StatefulSet configs) |
| V6 | Upgrade validation: drain a node, inject synthetic traffic, verify zero downtime |

---

## License

MIT
