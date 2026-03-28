package rules

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckMissingReadinessProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("deployment with no readiness probe is flagged HIGH", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		// minDeployment has no readiness probe by default
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingReadinessProbe(ctx, client, "default")
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
		if f.Rule != "missing-readiness-probe" {
			t.Errorf("want rule missing-readiness-probe, got %s", f.Rule)
		}
		if f.Fix != "" {
			t.Error("want empty Fix for missing-readiness-probe — fix requires app-specific knowledge")
		}
	})

	t.Run("deployment with readiness probe is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers[0].ReadinessProbe = httpProbe()
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingReadinessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings when probe present, got %d", len(findings))
		}
	})

	t.Run("multi-container pod: one finding even if only first container lacks probe", func(t *testing.T) {
		// First container has no probe, second does. Rule breaks after first hit —
		// one finding per workload, not one per container.
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers = []corev1.Container{
			{Name: "app", Image: "nginx:stable"}, // no probe
			{Name: "sidecar", Image: "envoy:stable", ReadinessProbe: httpProbe()},
		}
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingReadinessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Errorf("want exactly 1 finding (one per workload, not per container), got %d", len(findings))
		}
	})

	t.Run("multi-container pod: no finding when all containers have probes", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers = []corev1.Container{
			{Name: "app", Image: "nginx:stable", ReadinessProbe: httpProbe()},
			{Name: "sidecar", Image: "envoy:stable", ReadinessProbe: httpProbe()},
		}
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingReadinessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings, got %d", len(findings))
		}
	})

	t.Run("statefulset with no readiness probe is flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckMissingReadinessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Kind != "StatefulSet" {
			t.Errorf("want Kind=StatefulSet, got %s", findings[0].Kind)
		}
	})
}
