package rules

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckRiskyStatefulSet looks for StatefulSet configurations that make
// upgrades and node drains dangerous. Two patterns are flagged:
//
//  1. updateStrategy = OnDelete — Kubernetes will NOT automatically restart pods
//     when you update the spec. The old version keeps running silently until
//     someone manually deletes each pod. This is easy to forget and means your
//     "deployment" didn't actually deploy anything.
//
//  2. podManagementPolicy = Parallel with no PDB — by default StatefulSets
//     start/stop pods one at a time (OrderedReady). Parallel speeds this up but
//     removes the ordering guarantee, which matters for apps that rely on
//     peer discovery or leader election during startup.
func CheckRiskyStatefulSet(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}

	var findings []Finding

	for _, ss := range statefulsets.Items {
		// Rule 1: OnDelete update strategy
		if ss.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
			findings = append(findings, Finding{
				Namespace: ss.Namespace,
				Name:      ss.Name,
				Kind:      "StatefulSet",
				Rule:      "risky-statefulset",
				Severity:  SeverityHigh,
				Message: fmt.Sprintf(
					"StatefulSet %q uses updateStrategy=OnDelete. This means Kubernetes will NOT "+
						"automatically roll out spec changes — pods continue running the old version "+
						"until manually deleted. Upgrades silently have no effect. Switch to "+
						"RollingUpdate to get automatic, controlled rollouts.",
					ss.Name,
				),
			})
		}

		// Rule 2: Parallel pod management (warn, not error — it's a deliberate tradeoff)
		if ss.Spec.PodManagementPolicy == appsv1.ParallelPodManagement {
			findings = append(findings, Finding{
				Namespace: ss.Namespace,
				Name:      ss.Name,
				Kind:      "StatefulSet",
				Rule:      "risky-statefulset",
				Severity:  SeverityMedium,
				Message: fmt.Sprintf(
					"StatefulSet %q uses podManagementPolicy=Parallel. Pods are started and stopped "+
						"simultaneously instead of one at a time. For apps that need ordered startup "+
						"(e.g. databases doing leader election), this can cause split-brain or "+
						"startup failures. Only use Parallel if your app explicitly supports it.",
					ss.Name,
				),
			})
		}
	}

	return findings, nil
}
