package rules

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckMissingResources flags Deployments, StatefulSets, and DaemonSets where
// any container is missing CPU or memory requests/limits. Without resource
// requests, the Kubernetes scheduler cannot make informed placement decisions.
// Without limits, a misbehaving pod can consume all node resources during an
// upgrade, causing OOM kills and evictions of neighbouring pods.
func CheckMissingResources(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		for _, c := range d.Spec.Template.Spec.Containers {
			if detail, bad := resourceDetail(c.Resources); bad {
				findings = append(findings, Finding{
					Namespace: d.Namespace,
					Name:      d.Name,
					Kind:      "Deployment",
					Rule:      "missing-resources",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in Deployment %q is missing %s. Without resource requests, "+
							"the scheduler cannot make informed placement decisions — your pod may land "+
							"on an already-stressed node. Without limits, a misbehaving container can "+
							"consume all node resources and trigger OOM kills of neighbouring pods during "+
							"an upgrade.",
						c.Name, d.Name, detail,
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
			if detail, bad := resourceDetail(c.Resources); bad {
				findings = append(findings, Finding{
					Namespace: ss.Namespace,
					Name:      ss.Name,
					Kind:      "StatefulSet",
					Rule:      "missing-resources",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in StatefulSet %q is missing %s. Stateful workloads (databases, "+
							"queues) are especially vulnerable — an OOM kill mid-upgrade can corrupt "+
							"data or require manual recovery.",
						c.Name, ss.Name, detail,
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
			if detail, bad := resourceDetail(c.Resources); bad {
				findings = append(findings, Finding{
					Namespace: ds.Namespace,
					Name:      ds.Name,
					Kind:      "DaemonSet",
					Rule:      "missing-resources",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in DaemonSet %q is missing %s. DaemonSets run on every node — "+
							"an unconstrained DaemonSet can starve application pods across the entire "+
							"cluster during an upgrade.",
						c.Name, ds.Name, detail,
					),
				})
				break
			}
		}
	}

	return findings, nil
}

// resourceDetail returns a description of what's missing and true if any
// CPU or memory requests or limits are absent.
func resourceDetail(res corev1.ResourceRequirements) (string, bool) {
	missingRequests := res.Requests == nil ||
		res.Requests.Cpu().IsZero() ||
		res.Requests.Memory().IsZero()
	missingLimits := res.Limits == nil ||
		res.Limits.Cpu().IsZero() ||
		res.Limits.Memory().IsZero()

	switch {
	case missingRequests && missingLimits:
		return "CPU and memory requests and limits", true
	case missingRequests:
		return "CPU and memory requests", true
	case missingLimits:
		return "CPU and memory limits", true
	}
	return "", false
}
