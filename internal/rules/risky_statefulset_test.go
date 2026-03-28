package rules

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckRiskyStatefulSet(t *testing.T) {
	ctx := context.Background()

	t.Run("OnDelete strategy is flagged HIGH", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		ss.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
		}
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckRiskyStatefulSet(ctx, client, "default")
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
		if f.Fix != "" {
			t.Error("want empty Fix for risky-statefulset — fix requires app-specific knowledge")
		}
	})

	t.Run("Parallel pod management is flagged MEDIUM", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		ss.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckRiskyStatefulSet(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != SeverityMedium {
			t.Errorf("want MEDIUM severity, got %s", findings[0].Severity)
		}
	})

	t.Run("OnDelete + Parallel produces 2 findings", func(t *testing.T) {
		// Both are independent settings — both should fire separately.
		ss := minStatefulSet("my-db", "default", 2)
		ss.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
		}
		ss.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckRiskyStatefulSet(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 2 {
			t.Errorf("want 2 findings (one per issue), got %d", len(findings))
		}
	})

	t.Run("RollingUpdate + OrderedReady produces no findings", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		// minStatefulSet already sets RollingUpdate + OrderedReady
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckRiskyStatefulSet(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for safe StatefulSet, got %d", len(findings))
		}
	})
}
