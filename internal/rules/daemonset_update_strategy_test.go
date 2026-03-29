package rules

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckDaemonSetUpdateStrategy(t *testing.T) {
	ctx := context.Background()

	t.Run("daemonset with OnDelete strategy is flagged MEDIUM", func(t *testing.T) {
		ds := minDaemonSet("log-agent", "default")
		ds.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
			Type: appsv1.OnDeleteDaemonSetStrategyType,
		}
		client := fake.NewSimpleClientset(ds)

		findings, err := CheckDaemonSetUpdateStrategy(ctx, client, "default")
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
		if f.Rule != "daemonset-update-strategy" {
			t.Errorf("want rule daemonset-update-strategy, got %s", f.Rule)
		}
		if f.Fix != "" {
			t.Error("want empty Fix — fix is a simple spec change but we don't auto-patch DaemonSets yet")
		}
	})

	t.Run("daemonset with RollingUpdate strategy is not flagged", func(t *testing.T) {
		ds := minDaemonSet("log-agent", "default")
		// minDaemonSet uses RollingUpdate by default
		client := fake.NewSimpleClientset(ds)

		findings, err := CheckDaemonSetUpdateStrategy(ctx, client, "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("want 0 findings for RollingUpdate, got %d", len(findings))
		}
	})

	t.Run("finding kind is DaemonSet", func(t *testing.T) {
		ds := minDaemonSet("net-plugin", "kube-system")
		ds.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
			Type: appsv1.OnDeleteDaemonSetStrategyType,
		}
		client := fake.NewSimpleClientset(ds)

		findings, err := CheckDaemonSetUpdateStrategy(ctx, client, "")
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
