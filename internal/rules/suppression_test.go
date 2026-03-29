package rules

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestFilterSuppressed(t *testing.T) {
	ctx := context.Background()

	t.Run("finding is kept when no annotation present", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		client := fake.NewSimpleClientset(d)

		findings := []Finding{{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"}}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 1 || suppressed != 0 {
			t.Errorf("want 1 kept, 0 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("finding is suppressed when rule matches annotation", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Annotations = map[string]string{"kube-risk/suppress": "single-replica"}
		client := fake.NewSimpleClientset(d)

		findings := []Finding{{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"}}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 0 || suppressed != 1 {
			t.Errorf("want 0 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("only matching rule is suppressed, others kept", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Annotations = map[string]string{"kube-risk/suppress": "single-replica"}
		client := fake.NewSimpleClientset(d)

		findings := []Finding{
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"},
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "missing-pdb"},
		}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 1 || suppressed != 1 {
			t.Errorf("want 1 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
		if kept[0].Rule != "missing-pdb" {
			t.Errorf("want missing-pdb kept, got %s", kept[0].Rule)
		}
	})

	t.Run("comma-separated list suppresses multiple rules", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Annotations = map[string]string{"kube-risk/suppress": "single-replica, missing-pdb"}
		client := fake.NewSimpleClientset(d)

		findings := []Finding{
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"},
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "missing-pdb"},
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "unsafe-rollout"},
		}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 1 || suppressed != 2 {
			t.Errorf("want 1 kept, 2 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("wildcard * suppresses all findings for workload", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Annotations = map[string]string{"kube-risk/suppress": "*"}
		client := fake.NewSimpleClientset(d)

		findings := []Finding{
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"},
			{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "missing-pdb"},
		}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 0 || suppressed != 2 {
			t.Errorf("want 0 kept, 2 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("suppression on one workload does not affect another", func(t *testing.T) {
		d1 := minDeployment("app-a", "default", 1)
		d1.Annotations = map[string]string{"kube-risk/suppress": "single-replica"}
		d2 := minDeployment("app-b", "default", 1)
		client := fake.NewSimpleClientset(d1, d2)

		findings := []Finding{
			{Namespace: "default", Name: "app-a", Kind: "Deployment", Rule: "single-replica"},
			{Namespace: "default", Name: "app-b", Kind: "Deployment", Rule: "single-replica"},
		}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 1 || suppressed != 1 {
			t.Errorf("want 1 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
		if kept[0].Name != "app-b" {
			t.Errorf("want app-b kept, got %s", kept[0].Name)
		}
	})

	t.Run("suppress-reason annotation is ignored functionally", func(t *testing.T) {
		d := minDeployment("my-app", "default", 1)
		d.Annotations = map[string]string{
			"kube-risk/suppress":        "single-replica",
			"kube-risk/suppress-reason": "intentional — read-only cache",
		}
		client := fake.NewSimpleClientset(d)

		findings := []Finding{{Namespace: "default", Name: "my-app", Kind: "Deployment", Rule: "single-replica"}}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 0 || suppressed != 1 {
			t.Errorf("want 0 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("DaemonSet suppression works", func(t *testing.T) {
		ds := minDaemonSet("log-agent", "default")
		ds.Annotations = map[string]string{"kube-risk/suppress": "daemonset-update-strategy"}
		client := fake.NewSimpleClientset(ds)

		findings := []Finding{{Namespace: "default", Name: "log-agent", Kind: "DaemonSet", Rule: "daemonset-update-strategy"}}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 0 || suppressed != 1 {
			t.Errorf("want 0 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})

	t.Run("StatefulSet suppression works", func(t *testing.T) {
		ss := minStatefulSet("my-db", "default", 1)
		ss.Annotations = map[string]string{"kube-risk/suppress": "single-replica"}
		client := fake.NewSimpleClientset(ss)

		findings := []Finding{{Namespace: "default", Name: "my-db", Kind: "StatefulSet", Rule: "single-replica"}}
		kept, suppressed := filterSuppressed(ctx, client, findings)
		if len(kept) != 0 || suppressed != 1 {
			t.Errorf("want 0 kept, 1 suppressed; got %d kept, %d suppressed", len(kept), suppressed)
		}
	})
}

func TestIsSuppressed(t *testing.T) {
	cases := []struct {
		annotations map[string]string
		rule        string
		want        bool
	}{
		{nil, "single-replica", false},
		{map[string]string{}, "single-replica", false},
		{map[string]string{"kube-risk/suppress": "single-replica"}, "single-replica", true},
		{map[string]string{"kube-risk/suppress": "single-replica,missing-pdb"}, "missing-pdb", true},
		{map[string]string{"kube-risk/suppress": "single-replica, missing-pdb"}, "missing-pdb", true}, // spaces
		{map[string]string{"kube-risk/suppress": "*"}, "anything", true},
		{map[string]string{"kube-risk/suppress": "single-replica"}, "missing-pdb", false},
	}
	for _, tc := range cases {
		got := isSuppressed(tc.annotations, tc.rule)
		if got != tc.want {
			t.Errorf("isSuppressed(%v, %q) = %v, want %v", tc.annotations, tc.rule, got, tc.want)
		}
	}
}

