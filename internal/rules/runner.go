package rules

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// Runner holds the Kubernetes client and namespace scope, and runs all rules.
type Runner struct {
	Client    kubernetes.Interface
	Namespace string // "" means all namespaces
}

// RunAll executes every registered rule and aggregates the findings.
func (r *Runner) RunAll(ctx context.Context) ([]Finding, error) {
	allRules := []struct {
		name string
		fn   func(context.Context, kubernetes.Interface, string) ([]Finding, error)
	}{
		{"single-replica", CheckSingleReplica},
		{"missing-pdb", CheckMissingPDB},
		{"missing-readiness-probe", CheckMissingReadinessProbe},
		{"unsafe-rollout", CheckUnsafeRollout},
		{"risky-statefulset", CheckRiskyStatefulSet},
	}

	var findings []Finding
	for _, rule := range allRules {
		result, err := rule.fn(ctx, r.Client, r.Namespace)
		if err != nil {
			return nil, fmt.Errorf("rule %q failed: %w", rule.name, err)
		}
		findings = append(findings, result...)
	}
	ApplyScores(findings)
	return findings, nil
}
