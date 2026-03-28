package rules

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunnerDevModeFiltering(t *testing.T) {
	ctx := context.Background()

	// A deployment that triggers single-replica, missing-pdb, and missing-readiness-probe.
	d := minDeployment("my-app", "default", 1)

	t.Run("production mode runs all rules", func(t *testing.T) {
		client := fake.NewSimpleClientset(d)
		runner := &Runner{Client: client, Namespace: "default", Environment: "production"}

		findings, err := runner.RunAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rules := findingRules(findings)
		if !rules["single-replica"] {
			t.Error("production mode should run single-replica rule")
		}
		if !rules["missing-pdb"] {
			t.Error("production mode should run missing-pdb rule")
		}
		if !rules["missing-readiness-probe"] {
			t.Error("production mode should run missing-readiness-probe rule")
		}
	})

	t.Run("development mode skips single-replica and missing-pdb", func(t *testing.T) {
		client := fake.NewSimpleClientset(d)
		runner := &Runner{Client: client, Namespace: "default", Environment: "development"}

		findings, err := runner.RunAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rules := findingRules(findings)
		if rules["single-replica"] {
			t.Error("development mode should skip single-replica rule")
		}
		if rules["missing-pdb"] {
			t.Error("development mode should skip missing-pdb rule")
		}
		if !rules["missing-readiness-probe"] {
			t.Error("development mode should still run missing-readiness-probe rule")
		}
	})

	t.Run("RunAll sets scores on all findings", func(t *testing.T) {
		client := fake.NewSimpleClientset(d)
		runner := &Runner{Client: client, Namespace: "default", Environment: "production"}

		findings, err := runner.RunAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range findings {
			if f.Score == 0 {
				t.Errorf("finding %q/%q rule=%q has Score=0 — ApplyScores was not called",
					f.Namespace, f.Name, f.Rule)
			}
		}
	})

	t.Run("development mode still runs unsafe-rollout and risky-statefulset", func(t *testing.T) {
		// StatefulSet with OnDelete strategy — should fire even in dev mode.
		ss := minStatefulSet("my-db", "default", 1)
		ss.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
		}
		client := fake.NewSimpleClientset(ss)
		runner := &Runner{Client: client, Namespace: "default", Environment: "development"}

		findings, err := runner.RunAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rules := findingRules(findings)
		if !rules["risky-statefulset"] {
			t.Error("development mode should still run risky-statefulset rule")
		}
	})
}

// findingRules returns the set of rule IDs present in a slice of findings.
func findingRules(findings []Finding) map[string]bool {
	rules := make(map[string]bool)
	for _, f := range findings {
		rules[f.Rule] = true
	}
	return rules
}
