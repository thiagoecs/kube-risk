package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckMissingPDB(t *testing.T) {
	ctx := context.Background()

	t.Run("deployment with no PDB is flagged MEDIUM", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingPDB(ctx, client, "default")
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
		if f.Rule != "missing-pdb" {
			t.Errorf("want rule missing-pdb, got %s", f.Rule)
		}
	})

	t.Run("deployment with matching PDB is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		pdb := minPDB("my-app-pdb", "default", map[string]string{"app": "my-app"})
		client := fake.NewSimpleClientset(d, pdb)

		findings, err := CheckMissingPDB(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings when PDB covers deployment, got %d", len(findings))
		}
	})

	t.Run("PDB with subset of pod labels still covers the deployment", func(t *testing.T) {
		// Pod has labels {app: my-app, version: v1}
		// PDB only selects on {app: my-app} — this is valid because PDB matchLabels
		// is a subset match: all PDB labels must appear in the pod labels.
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Labels["version"] = "v1"
		pdb := minPDB("my-app-pdb", "default", map[string]string{"app": "my-app"})
		client := fake.NewSimpleClientset(d, pdb)

		findings, err := CheckMissingPDB(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings when PDB matchLabels is a subset of pod labels, got %d", len(findings))
		}
	})

	t.Run("PDB in different namespace does not cover deployment", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		pdb := minPDB("my-app-pdb", "other-ns", map[string]string{"app": "my-app"})
		client := fake.NewSimpleClientset(d, pdb)

		findings, err := CheckMissingPDB(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Errorf("want 1 finding when PDB is in different namespace, got %d", len(findings))
		}
	})

	t.Run("statefulset with no PDB is flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		client := fake.NewSimpleClientset(ss)

		findings, err := CheckMissingPDB(ctx, client, "default")
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

	t.Run("statefulset with matching PDB is not flagged", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 2)
		pdb := minPDB("my-db-pdb", "default", map[string]string{"app": "my-db"})
		client := fake.NewSimpleClientset(ss, pdb)

		findings, err := CheckMissingPDB(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings, got %d", len(findings))
		}
	})

	t.Run("fix YAML contains workload name, namespace, and pod labels", func(t *testing.T) {
		d := minDeployment("frontend", "payments", 2)
		client := fake.NewSimpleClientset(d)

		findings, err := CheckMissingPDB(ctx, client, "payments")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		fix := findings[0].Fix
		for _, want := range []string{"frontend", "payments", "app:", "PodDisruptionBudget"} {
			if !contains(fix, want) {
				t.Errorf("fix should contain %q\nfix: %s", want, fix)
			}
		}
	})
}
