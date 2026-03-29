package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckHPAMinReplicas(t *testing.T) {
	ctx := context.Background()

	t.Run("HPA with minReplicas=1 is flagged HIGH", func(t *testing.T) {
		hpa := minHPA("my-hpa", "default", "my-app", 1, 10)
		client := fake.NewSimpleClientset(hpa)

		findings, err := CheckHPAMinReplicas(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		f := findings[0]
		if f.Severity != SeverityHigh {
			t.Errorf("want HIGH severity, got %s", f.Severity)
		}
		if f.Rule != "hpa-min-replicas" {
			t.Errorf("want rule hpa-min-replicas, got %s", f.Rule)
		}
	})

	t.Run("HPA with minReplicas=2 is not flagged", func(t *testing.T) {
		hpa := minHPA("my-hpa", "default", "my-app", 2, 10)
		client := fake.NewSimpleClientset(hpa)

		findings, err := CheckHPAMinReplicas(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for minReplicas=2, got %d", len(findings))
		}
	})

	t.Run("HPA with minReplicas=3 is not flagged", func(t *testing.T) {
		hpa := minHPA("my-hpa", "default", "my-app", 3, 10)
		client := fake.NewSimpleClientset(hpa)

		findings, err := CheckHPAMinReplicas(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for minReplicas=3, got %d", len(findings))
		}
	})

	t.Run("finding points to the HPA itself so it can be auto-patched", func(t *testing.T) {
		hpa := minHPA("my-hpa", "default", "my-app", 1, 10)
		client := fake.NewSimpleClientset(hpa)

		findings, err := CheckHPAMinReplicas(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Name != "my-hpa" {
			t.Errorf("want finding Name=my-hpa (the HPA), got %s", findings[0].Name)
		}
		if findings[0].Kind != "HorizontalPodAutoscaler" {
			t.Errorf("want Kind=HorizontalPodAutoscaler, got %s", findings[0].Kind)
		}
	})

	t.Run("finding Fix is set", func(t *testing.T) {
		hpa := minHPA("my-hpa", "default", "my-app", 1, 10)
		client := fake.NewSimpleClientset(hpa)

		findings, err := CheckHPAMinReplicas(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Fix == "" {
			t.Error("want non-empty Fix for hpa-min-replicas")
		}
	})
}
