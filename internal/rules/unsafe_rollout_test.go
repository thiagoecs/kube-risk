package rules

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckUnsafeRollout(t *testing.T) {
	ctx := context.Background()

	t.Run("maxUnavailable=3 of 4 replicas is flagged MEDIUM", func(t *testing.T) {
		d := minDeployment("my-app", "default", 4)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrInt(3)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		f := findings[0]
		if f.Severity != SeverityMedium {
			t.Errorf("want MEDIUM severity, got %s", f.Severity)
		}
		if f.Fix == "" {
			t.Error("want non-empty Fix for unsafe-rollout")
		}
	})

	t.Run("maxUnavailable=1 of 4 replicas is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 4)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrInt(1)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for maxUnavailable=1 of 4, got %d", len(findings))
		}
	})

	t.Run("maxUnavailable=1 of 2 replicas is flagged (50% offline)", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrInt(1)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Errorf("want 1 finding for maxUnavailable=1 of 2 (50%%), got %d", len(findings))
		}
	})

	t.Run("percentage maxUnavailable=75%% of 4 replicas is flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 4)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrPercent("75%")
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding for 75%% maxUnavailable, got %d", len(findings))
		}
	})

	t.Run("Recreate strategy is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 4)
		d.Spec.Strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType,
		}
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for Recreate strategy (intentional), got %d", len(findings))
		}
	})

	t.Run("single replica deployment is not flagged", func(t *testing.T) {
		// The rule requires replicas > 1 to fire — single replica deployments
		// are already caught by single-replica rule.
		d := minDeployment("my-app", "default", 1)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrInt(1)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for single replica, got %d", len(findings))
		}
	})

	t.Run("fix contains workload name and namespace", func(t *testing.T) {
		d := minDeployment("backend", "prod", 4)
		d.Spec.Strategy.RollingUpdate.MaxUnavailable = intstrInt(3)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckUnsafeRollout(ctx, client, "prod")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		fix := findings[0].Fix
		for _, want := range []string{"backend", "prod", "maxUnavailable"} {
			if !contains(fix, want) {
				t.Errorf("fix should contain %q\nfix: %s", want, fix)
			}
		}
	})
}

func TestResolveIntOrPercent(t *testing.T) {
	t.Run("nil defaults to 25%% of total", func(t *testing.T) {
		if got := resolveIntOrPercent(nil, 4); got != 1 {
			t.Errorf("want 1, got %d", got)
		}
	})
	t.Run("absolute integer is returned as-is", func(t *testing.T) {
		if got := resolveIntOrPercent(intstrInt(3), 4); got != 3 {
			t.Errorf("want 3, got %d", got)
		}
	})
	t.Run("75%% of 4 = 3", func(t *testing.T) {
		if got := resolveIntOrPercent(intstrPercent("75%"), 4); got != 3 {
			t.Errorf("want 3, got %d", got)
		}
	})
	t.Run("50%% of 4 = 2", func(t *testing.T) {
		if got := resolveIntOrPercent(intstrPercent("50%"), 4); got != 2 {
			t.Errorf("want 2, got %d", got)
		}
	})
}
