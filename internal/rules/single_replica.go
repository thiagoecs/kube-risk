package rules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckSingleReplica flags any Deployment or StatefulSet running with exactly
// one replica. During a node drain (which happens on every cluster upgrade),
// Kubernetes must evict that pod. With no second copy running, the app goes
// down while the new pod is scheduling — that's avoidable downtime.
func CheckSingleReplica(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		replicas := int32(1)
		if d.Spec.Replicas != nil {
			replicas = *d.Spec.Replicas
		}
		if replicas <= 1 {
			findings = append(findings, Finding{
				Namespace: d.Namespace,
				Name:      d.Name,
				Kind:      "Deployment",
				Rule:      "single-replica",
				Severity:  SeverityHigh,
				Message: fmt.Sprintf(
					"Deployment %q has only %d replica. During a node drain (e.g. cluster upgrade), "+
						"this pod will be evicted and the app will be unavailable until the new pod starts. "+
						"Set replicas >= 2 to ensure continuity.",
					d.Name, replicas,
				),
				Fix: fmt.Sprintf(
					"kubectl scale deployment %s -n %s --replicas=2\n\n"+
						"Why 2: the minimum needed to keep one pod running while another is\n"+
						"evicted during a node drain. Scale higher based on your traffic needs.",
					d.Name, d.Namespace,
				),
			})
		}
	}

	// Check StatefulSets
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	for _, ss := range statefulsets.Items {
		replicas := int32(1)
		if ss.Spec.Replicas != nil {
			replicas = *ss.Spec.Replicas
		}
		if replicas <= 1 {
			findings = append(findings, Finding{
				Namespace: ss.Namespace,
				Name:      ss.Name,
				Kind:      "StatefulSet",
				Rule:      "single-replica",
				Severity:  SeverityHigh,
				Message: fmt.Sprintf(
					"StatefulSet %q has only %d replica. StatefulSets with a single replica have "+
						"no redundancy — any pod disruption means complete downtime. "+
						"Consider scaling to >= 2 replicas if the workload supports it.",
					ss.Name, replicas,
				),
				Fix: fmt.Sprintf(
					"kubectl patch statefulset %s -n %s -p '{\"spec\":{\"replicas\":2}}'\n\n"+
						"Why 2: the minimum for redundancy. Before applying, verify your app\n"+
						"supports multiple replicas — some databases need extra configuration\n"+
						"for replication or leader election.",
					ss.Name, ss.Namespace,
				),
			})
		}
	}

	return findings, nil
}
