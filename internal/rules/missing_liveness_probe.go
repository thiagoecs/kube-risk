package rules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckMissingLivenessProbe flags Deployments, StatefulSets, and DaemonSets
// where any container is missing a liveness probe. Without a liveness probe,
// Kubernetes has no way to detect a deadlocked or hung process — the container
// stays Running forever while serving no useful work. During a cluster upgrade,
// broken pods that never restart block the upgrade from completing cleanly.
func CheckMissingLivenessProbe(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.LivenessProbe == nil {
				findings = append(findings, Finding{
					Namespace: d.Namespace,
					Name:      d.Name,
					Kind:      "Deployment",
					Rule:      "missing-liveness-probe",
					Severity:  SeverityHigh,
					Message: fmt.Sprintf(
						"Container %q in Deployment %q has no liveness probe. If the process deadlocks "+
							"or hangs, Kubernetes will never restart it — the pod stays in Running state "+
							"while serving no useful work. Add a livenessProbe so Kubernetes can detect "+
							"and recover from stuck processes automatically.",
						c.Name, d.Name,
					),
				})
				break
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
			if c.LivenessProbe == nil {
				findings = append(findings, Finding{
					Namespace: ss.Namespace,
					Name:      ss.Name,
					Kind:      "StatefulSet",
					Rule:      "missing-liveness-probe",
					Severity:  SeverityHigh,
					Message: fmt.Sprintf(
						"Container %q in StatefulSet %q has no liveness probe. For stateful workloads "+
							"like databases, a hung process that never restarts can cause cascading "+
							"failures — clients pile up waiting for connections that will never be served.",
						c.Name, ss.Name,
					),
				})
				break
			}
		}
	}

	// Check DaemonSets
	daemonsets, err := client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing daemonsets: %w", err)
	}
	for _, ds := range daemonsets.Items {
		for _, c := range ds.Spec.Template.Spec.Containers {
			if c.LivenessProbe == nil {
				findings = append(findings, Finding{
					Namespace: ds.Namespace,
					Name:      ds.Name,
					Kind:      "DaemonSet",
					Rule:      "missing-liveness-probe",
					Severity:  SeverityHigh,
					Message: fmt.Sprintf(
						"Container %q in DaemonSet %q has no liveness probe. DaemonSets run on every "+
							"node — a hung process on any node will go undetected and unrecovered until "+
							"someone notices and manually restarts the pod.",
						c.Name, ds.Name,
					),
				})
				break
			}
		}
	}

	return findings, nil
}
