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
| `missing-liveness-probe` | HIGH | Containers without a liveness probe — stuck pods are never restarted automatically | all |
| `hpa-min-replicas` | HIGH | HPA with `minReplicas=1` — silently defeats replica count and PDB protections | all |
| `risky-statefulset` | HIGH/MEDIUM | `OnDelete` update strategy or `Parallel` pod management | all |
| `missing-pdb` | MEDIUM | No PodDisruptionBudget — Kubernetes can evict all pods simultaneously | production only |
| `unsafe-rollout` | MEDIUM | `maxUnavailable` ≥ 50% of replicas — too much capacity offline during updates | all |
| `missing-resources` | MEDIUM | Containers without CPU/memory requests or limits — node pressure causes evictions | all |
| `latest-image-tag` | MEDIUM | Containers using `:latest` or no tag — non-deterministic deploys, hard to roll back | all |
| `daemonset-update-strategy` | MEDIUM | DaemonSet using `OnDelete` — node agents never update automatically | all |

---

## Installation

**Download a pre-built binary:**

```bash
curl -sSL https://github.com/thiagoecs/kube-risk/releases/download/v0.10.0/kube-risk-linux-amd64 \
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

One PR is opened per workload with fixable findings. For workloads where every finding requires manual attention, a **GitHub Issue** is opened instead — so nothing gets silently dropped.

**Fixable automatically (PR):** `single-replica`, `unsafe-rollout`, `missing-pdb`, `hpa-min-replicas`, `daemonset-update-strategy`

**Require manual fix (Issue):** `missing-readiness-probe`, `missing-liveness-probe`, `risky-statefulset`, `latest-image-tag`, `missing-resources` — these depend on app-specific knowledge. LLM-assisted fixes are planned for a future version.

### Manifest format support

| Format | `analyze` | Auto-fix PRs |
|--------|-----------|--------------|
| Raw YAML | ✓ | ✓ |
| Kustomize | ✓ | ✗ (planned) |
| Helm | ✓ | ✗ (planned) |

`analyze` works for all formats because it reads the live cluster directly. Auto-fix PRs currently require raw YAML files in the repo — kube-risk needs a file to patch. Helm and Kustomize support is planned: Helm requires patching `values.yaml`, Kustomize requires patching the right overlay. Both are on the roadmap.

---

## GitHub Action

The action supports two modes. Use both together for full coverage.

### Mode: `analyze` — CI gate

Fails the job if HIGH findings exist. Add this to block merges that leave the cluster in a risky state.

Copy [`examples/kube-risk-gate.yml`](examples/kube-risk-gate.yml) to `.github/workflows/kube-risk-gate.yml`:

```yaml
- uses: thiagoecs/kube-risk@v0.10.0
  with:
    mode: analyze
    kubeconfig: ${{ secrets.KUBECONFIG }}
    namespace: production
    environment: production
```

Triggers on push and pull_request. Only needs `KUBECONFIG` — no GitHub token required.

### Mode: `pr` — scheduled fix opener

Runs on a schedule and opens PRs for auto-fixable findings and GitHub Issues for findings that require manual attention. Idempotent — safe to run nightly, won't create duplicates.

Copy [`examples/kube-risk-action.yml`](examples/kube-risk-action.yml) to `.github/workflows/kube-risk.yml`:

```yaml
- uses: thiagoecs/kube-risk@v0.10.0
  with:
    mode: pr
    kubeconfig: ${{ secrets.KUBECONFIG }}
    github-token: ${{ secrets.GITHUB_TOKEN }}
    repo: ${{ github.repository }}
    namespace: production
    environment: production
```

**Generating a scoped KUBECONFIG (recommended):**

kube-risk only needs to read your cluster — it never modifies it. Create a minimal service account instead of using your admin kubeconfig:

```bash
# Install the read-only service account (one-time setup)
kubectl apply -f https://github.com/thiagoecs/kube-risk/releases/download/v0.10.0/rbac.yaml

# Generate a minimal kubeconfig and copy the output
curl -sSL https://github.com/thiagoecs/kube-risk/releases/download/v0.10.0/get-kubeconfig.sh | bash
```

The service account has read-only access to workloads — it cannot read secrets, exec into pods, or modify anything.

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
