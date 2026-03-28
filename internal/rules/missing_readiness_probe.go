package rules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckMissingReadinessProbe flags Deployments and StatefulSets where any
// container is missing a readiness probe. Kubernetes uses the readiness probe
// to decide when a pod is ready to receive traffic. Without one, Kubernetes
// assumes the pod is ready the moment it starts — before the app has actually
// finished initializing. During a rolling update this means traffic is routed
// to pods that aren't ready yet, causing errors for real users.
func CheckMissingReadinessProbe(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.ReadinessProbe == nil {
				findings = append(findings, Finding{
					Namespace: d.Namespace,
					Name:      d.Name,
					Kind:      "Deployment",
					Rule:      "missing-readiness-probe",
					Severity:  SeverityHigh,
					Message: fmt.Sprintf(
						"Container %q in Deployment %q has no readiness probe. Kubernetes will send "+
							"live traffic to this container as soon as it starts, even if the app isn't "+
							"ready yet. Add a readinessProbe (httpGet, tcpSocket, or exec) so Kubernetes "+
							"knows when it's safe to route requests.",
						c.Name, d.Name,
					),
				})
				break // one finding per workload is enough
			}
		}
	}

	// Check StatefulSets
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	for _, ss := range statefulsets.Items {
		for _, c := range ss.Spec.Template.Spec.Containers {
			if c.ReadinessProbe == nil {
				findings = append(findings, Finding{
					Namespace: ss.Namespace,
					Name:      ss.Name,
					Kind:      "StatefulSet",
					Rule:      "missing-readiness-probe",
					Severity:  SeverityHigh,
					Message: fmt.Sprintf(
						"Container %q in StatefulSet %q has no readiness probe. Without this, "+
							"traffic can reach the pod before the app (e.g. database) is accepting "+
							"connections, leading to connection errors during startup or rolling restarts.",
						c.Name, ss.Name,
					),
				})
				break
			}
		}
	}

	return findings, nil
}
