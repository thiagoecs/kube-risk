package rules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// CheckUnsafeRollout flags Deployments whose rolling update strategy allows
// too many pods to be unavailable at the same time. The default in Kubernetes
// is maxUnavailable=25%, which is fine for large deployments but can mean zero
// running pods for small ones (e.g. 1 replica × 25% rounds down to 0, so
// Kubernetes actually uses 1 — the whole deployment). We flag cases where
// maxUnavailable resolves to >= 50% of the replica count.
func CheckUnsafeRollout(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}

	for _, d := range deployments.Items {
		strategy := d.Spec.Strategy
		if strategy.RollingUpdate == nil {
			// Non-rolling strategy (Recreate) takes everything down at once — that's
			// intentional, so we skip it here (the user chose it explicitly).
			continue
		}

		replicas := int32(1)
		if d.Spec.Replicas != nil {
			replicas = *d.Spec.Replicas
		}

		maxUnavailable := resolveIntOrPercent(strategy.RollingUpdate.MaxUnavailable, replicas)

		// Flag when maxUnavailable is >= half the replica count, because that
		// means most of the capacity can be gone simultaneously.
		if replicas > 1 && maxUnavailable >= (replicas+1)/2 {
			findings = append(findings, Finding{
				Namespace: d.Namespace,
				Name:      d.Name,
				Kind:      "Deployment",
				Rule:      "unsafe-rollout",
				Severity:  SeverityMedium,
				Message: fmt.Sprintf(
					"Deployment %q allows %d of %d pods to be unavailable during a rolling update "+
						"(maxUnavailable=%v). That's %.0f%% of capacity offline simultaneously. "+
						"Consider lowering maxUnavailable to 1 or 25%% to reduce blast radius.",
					d.Name, maxUnavailable, replicas,
					strategy.RollingUpdate.MaxUnavailable,
					float64(maxUnavailable)/float64(replicas)*100,
				),
				Fix: fmt.Sprintf(
					"kubectl patch deployment %s -n %s \\\n"+
						"  --type=json \\\n"+
						"  -p='[{\"op\":\"replace\",\"path\":\"/spec/strategy/rollingUpdate/maxUnavailable\",\"value\":1}]'\n\n"+
						"Why 1: allows updates to proceed one pod at a time, keeping all other\n"+
						"replicas serving traffic. Safe for any replica count >= 2.",
					d.Name, d.Namespace,
				),
			})
		}
	}

	return findings, nil
}

// resolveIntOrPercent converts a Kubernetes IntOrString (which can be an
// absolute integer or a percentage string like "50%") into an absolute count
// relative to the given total.
func resolveIntOrPercent(val *intstr.IntOrString, total int32) int32 {
	if val == nil {
		// Kubernetes default: 25%
		return ((total * 25) + 99) / 100
	}
	if val.Type == intstr.Int {
		return val.IntVal
	}
	// Percentage: parse the number before "%"
	pct := int32(0)
	fmt.Sscanf(val.StrVal, "%d%%", &pct)
	return (total * pct) / 100
}
