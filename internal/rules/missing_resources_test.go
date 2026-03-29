package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckMissingResources(t *testing.T) {
	ctx := context.Background()

	t.Run("deployment with no resources is flagged MEDIUM", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		// minDeployment has no resources by default
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingResources(ctx, client, "default")
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
		if f.Rule != "missing-resources" {
			t.Errorf("want rule missing-resources, got %s", f.Rule)
		}
	})

	t.Run("deployment with full resources is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers[0].Resources = resourceRequirements()
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingResources(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings when resources present, got %d", len(findings))
		}
	})

	t.Run("daemonset with no resources is flagged", func(t *testing.T) {
		ds := minDaemonSet("log-agent", "default")
		client := fake.NewSimpleClientset(ds)

		findings, err := CheckMissingResources(ctx, client, "default")
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

	t.Run("statefulset with no resources is flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckMissingResources(ctx, client, "default")
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
