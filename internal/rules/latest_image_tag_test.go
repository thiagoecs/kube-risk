package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckLatestImageTag(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		image   string
		flagged bool
	}{
		{"nginx", true},              // no tag — defaults to latest
		{"nginx:latest", true},       // explicit latest
		{"nginx:1.25.3", false},      // pinned version
		{"nginx:stable", false},      // named stable tag (not latest)
		{"registry.io/nginx:latest", true},  // registry + latest
		{"registry.io/nginx:1.25.3", false}, // registry + pinned
		{"nginx@sha256:abc123", false},      // digest — always immutable
	}

	for _, tc := range cases {
		t.Run(tc.image, func(t *testing.T) {
			d := minDeployment("my-app", "default", 2)
			d.Spec.Template.Spec.Containers[0].Image = tc.image
			client := fake.NewSimpleClientset(d)

			findings, err := CheckLatestImageTag(ctx, client, "default")
			if err != nil {
				t.Fatal(err)
			}
			if tc.flagged && len(findings) == 0 {
				t.Errorf("image %q should be flagged but wasn't", tc.image)
			}
			if !tc.flagged && len(findings) > 0 {
				t.Errorf("image %q should not be flagged but was", tc.image)
			}
		})
	}

	t.Run("pinned deployment is not flagged", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		// minDeployment uses nginx:stable — should not be flagged
		client := fake.NewSimpleClientset(d)

		findings, err := CheckLatestImageTag(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for pinned image, got %d", len(findings))
		}
	})

	t.Run("finding is MEDIUM severity with no fix", func(t *testing.T) {
		d := minDeployment("my-app", "default", 2)
		d.Spec.Template.Spec.Containers[0].Image = "nginx:latest"
		client := fake.NewSimpleClientset(d)

		findings, err := CheckLatestImageTag(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("want 1 finding, got %d", len(findings))
		}
		if findings[0].Severity != SeverityMedium {
			t.Errorf("want MEDIUM, got %s", findings[0].Severity)
		}
		if findings[0].Fix != "" {
			t.Error("want empty Fix — correct tag requires app-specific knowledge")
		}
	})
}
