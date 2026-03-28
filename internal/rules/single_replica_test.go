package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckSingleReplica(t *testing.T) {
	ctx := context.Background()

	t.Run("deployment with 1 replica is flagged HIGH", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckSingleReplica(ctx, client, "default")
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
		if f.Rule != "single-replica" {
			t.Errorf("want rule single-replica, got %s", f.Rule)
		}
		if f.Fix == "" {
			t.Error("want non-empty Fix for single-replica deployment")
		}
	})

	t.Run("deployment with nil replicas (defaults to 1) is flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Spec.Replicas = nil // nil means 1 in Kubernetes
		client := fake.NewSimpleClientset(d)

		findings, err := CheckSingleReplica(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding for nil replicas, got %d", len(findings))
		}
	})

	t.Run("deployment with 2 replicas is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckSingleReplica(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for 2 replicas, got %d", len(findings))
		}
	})

	t.Run("statefulset with 1 replica is flagged HIGH", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 1)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckSingleReplica(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		f := findings[0]
		if f.Kind != "StatefulSet" {
			t.Errorf("want Kind=StatefulSet, got %s", f.Kind)
		}
		if f.Fix == "" {
			t.Error("want non-empty Fix for single-replica statefulset")
		}
	})

	t.Run("statefulset with 3 replicas is not flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 3)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckSingleReplica(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings, got %d", len(findings))
		}
	})

	t.Run("fix contains workload name and namespace", func(t *testing.T) {
		d := minDeployment("frontend", "payments", 1)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckSingleReplica(ctx, client, "payments")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		fix := findings[0].Fix
		if !contains(fix, "frontend") {
			t.Errorf("fix should contain workload name 'frontend', got: %s", fix)
		}
		if !contains(fix, "payments") {
			t.Errorf("fix should contain namespace 'payments', got: %s", fix)
		}
	})
}

