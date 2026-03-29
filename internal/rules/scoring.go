package rules

import "strings"

// baseScores maps "rule:severity" to a base numeric score (1–10).
// Using the compound key lets us distinguish findings from the same rule
// that have different severities (e.g. risky-statefulset fires at both HIGH
// for OnDelete and MEDIUM for Parallel pod management).
//
// Score philosophy:
//   single-replica      9 — guaranteed downtime on any node drain
//   risky-statefulset   8 — OnDelete means upgrades silently do nothing
//   missing-readiness   7 — live traffic hits unready pods on every rollout
//   missing-pdb         5 — no protection from simultaneous eviction
//   unsafe-rollout      4 — large blast radius but at least it's a rolling update
//   risky-statefulset   4 — Parallel is a tradeoff, not always wrong
var baseScores = map[string]int{
	"single-replica:HIGH":              9,
	"risky-statefulset:HIGH":           8,
	"missing-liveness-probe:HIGH":      7,
	"missing-readiness-probe:HIGH":     7,
	"hpa-min-replicas:HIGH":            7, // silently defeats replica+PDB fixes
	"missing-pdb:MEDIUM":               5,
	"risky-statefulset:MEDIUM":         4,
	"unsafe-rollout:MEDIUM":            4,
	"daemonset-update-strategy:MEDIUM": 4,
	"missing-resources:MEDIUM":         3,
	"latest-image-tag:MEDIUM":          3,
}

// namespaceBoost returns a score adjustment based on how production-like the
// namespace name looks. Kubernetes naming conventions vary, but "prod" and
// "live" are universally understood as production, while "dev/staging/test"
// are lower-stakes environments.
func namespaceBoost(namespace string) int {
	ns := strings.ToLower(namespace)
	if strings.Contains(ns, "prod") || strings.Contains(ns, "live") {
		return +2 // production environments have real user impact
	}
	if strings.Contains(ns, "dev") || strings.Contains(ns, "staging") || strings.Contains(ns, "test") {
		return -1 // lower-stakes environments
	}
	return 0
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ApplyScores computes and sets the Score field on each finding.
// Call this once after all rules have run.
// Namespace boost is skipped in development mode — namespace names in dev
// clusters are arbitrary and don't reflect production risk.
func ApplyScores(findings []Finding, environment string) {
	for i := range findings {
		f := &findings[i]
		key := f.Rule + ":" + string(f.Severity)
		base, ok := baseScores[key]
		if !ok {
			// Fallback for any future rules that don't have an explicit entry
			switch f.Severity {
			case SeverityHigh:
				base = 7
			case SeverityMedium:
				base = 4
			default:
				base = 2
			}
		}
		boost := 0
		if environment != "development" {
			boost = namespaceBoost(f.Namespace)
		}
		f.Score = clamp(base+boost, 1, 10)
	}
}
