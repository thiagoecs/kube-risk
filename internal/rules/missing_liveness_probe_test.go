package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckMissingLivenessProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("deployment with no liveness probe is flagged HIGH", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingLivenessProbe(ctx, client, "default")
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
		if f.Rule != "missing-liveness-probe" {
			t.Errorf("want rule missing-liveness-probe, got %s", f.Rule)
		}
		if f.Fix != "" {
			t.Error("want empty Fix — liveness probe fix requires app-specific knowledge")
		}
	})

	t.Run("deployment with liveness probe is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers[0].LivenessProbe = httpProbe()
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingLivenessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings when probe present, got %d", len(findings))
		}
	})

	t.Run("statefulset with no liveness probe is flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckMissingLivenessProbe(ctx, client, "default")
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

	t.Run("daemonset with no liveness probe is flagged", func(t *testing.T) {
		ds := minDaemonSet("log-agent", "default")
		client := fake.NewSimpleClientset(ds)

		findings, err := CheckMissingLivenessProbe(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Kind != "DaemonSet" {
			t.Errorf("want Kind=DaemonSet, got %s", findings[0].Kind)
		}
	})
}
