package rules

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// Runner holds the Kubernetes client and namespace scope, and runs all rules.
type Runner struct {
	Client      kubernetes.Interface
	Namespace   string // "" means all namespaces
	Environment string // "production" (default) or "development"
}

// devSkipRules lists rules that produce noise in development environments.
// Single replicas and missing PDBs are intentional in dev — flagging them
// trains operators to ignore the tool. Config quality rules (readiness probes,
// rollout strategy, StatefulSet config) still run because those bugs will
// follow the config into production if not caught here.
var devSkipRules = map[string]bool{
	"single-replica": true,
	"missing-pdb":    true,
}

// RunAll executes every registered rule, filters suppressed findings, and
// returns the active findings plus the count of suppressed ones.
func (r *Runner) RunAll(ctx context.Context) ([]Finding, int, error) {
	allRules := []struct {
		name string
		fn   func(context.Context, kubernetes.Interface, string) ([]Finding, error)
	}{
		{"single-replica", CheckSingleReplica},
		{"missing-pdb", CheckMissingPDB},
		{"missing-readiness-probe", CheckMissingReadinessProbe},
		{"missing-liveness-probe", CheckMissingLivenessProbe},
		{"unsafe-rollout", CheckUnsafeRollout},
		{"risky-statefulset", CheckRiskyStatefulSet},
		{"missing-resources", CheckMissingResources},
		{"latest-image-tag", CheckLatestImageTag},
		{"hpa-min-replicas", CheckHPAMinReplicas},
		{"daemonset-update-strategy", CheckDaemonSetUpdateStrategy},
	}

	var findings []Finding
	for _, rule := range allRules {
		if r.Environment == "development" && devSkipRules[rule.name] {
			continue
		}
		result, err := rule.fn(ctx, r.Client, r.Namespace)
		if err != nil {
			return nil, 0, fmt.Errorf("rule %q failed: %w", rule.name, err)
		}
		findings = append(findings, result...)
	}
	findings, suppressed := filterSuppressed(ctx, r.Client, findings)
	ApplyScores(findings, r.Environment)
	return findings, suppressed, nil
}
