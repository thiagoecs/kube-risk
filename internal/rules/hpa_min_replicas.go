package rules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckHPAMinReplicas flags HorizontalPodAutoscalers where minReplicas is 1
// (or unset, which defaults to 1). An HPA with minReplicas=1 can silently scale
// your Deployment back down to 1 replica during low-traffic periods — defeating
// any PDB or replica count fix and leaving the workload vulnerable to downtime
// during the next node drain or cluster upgrade.
func CheckHPAMinReplicas(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	hpas, err := client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// Fall back to v1 if v2 is unavailable (older clusters)
		return checkHPAV1(ctx, client, namespace)
	}

	var findings []Finding
	for _, hpa := range hpas.Items {
		minReplicas := int32(1) // Kubernetes default when unset
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}
		if minReplicas <= 1 {
			findings = append(findings, hpaFinding(hpa.Namespace, hpa.Name, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name, minReplicas))
		}
	}
	return findings, nil
}

// checkHPAV1 is a fallback for clusters that don't support autoscaling/v2.
func checkHPAV1(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	hpas, err := client.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing HPAs: %w", err)
	}

	var findings []Finding
	for _, hpa := range hpas.Items {
		minReplicas := int32(1)
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}
		if minReplicas <= 1 {
			findings = append(findings, hpaFinding(hpa.Namespace, hpa.Name, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name, minReplicas))
		}
	}
	return findings, nil
}

func hpaFinding(namespace, hpaName, targetKind, targetName string, minReplicas int32) Finding {
	return Finding{
		Namespace: namespace,
		Name:      hpaName,
		Kind:      "HorizontalPodAutoscaler",
		Rule:      "hpa-min-replicas",
		Severity:  SeverityHigh,
		Message: fmt.Sprintf(
			"HPA %q targets %s %q with minReplicas=%d. During low-traffic periods, the HPA "+
				"will scale the workload down to 1 replica — making any replica count or PDB "+
				"fix ineffective. When a node drain then occurs, that single pod is evicted "+
				"and the workload goes down. Set minReplicas >= 2 to ensure there is always "+
				"a fallback pod during drains.",
			hpaName, targetKind, targetName, minReplicas,
		),
		Fix: fmt.Sprintf(
			"kubectl patch hpa %s -n %s "+
				"--type=json -p='[{\"op\":\"replace\",\"path\":\"/spec/minReplicas\",\"value\":2}]'\n\n"+
				"Why 2: ensures at least one pod is always available as a fallback during\n"+
				"node drains, regardless of traffic levels.",
			hpaName, namespace,
		),
	}
}
