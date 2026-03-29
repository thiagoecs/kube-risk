package rules

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckDaemonSetUpdateStrategy flags DaemonSets using the OnDelete update
// strategy. Like StatefulSets with OnDelete, this means spec changes are never
// automatically applied — pods continue running the old version until manually
// deleted. During a cluster upgrade, this can leave monitoring agents, log
// collectors, or network plugins running stale code on upgraded nodes.
func CheckDaemonSetUpdateStrategy(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	daemonsets, err := client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing daemonsets: %w", err)
	}

	var findings []Finding
	for _, ds := range daemonsets.Items {
		if ds.Spec.UpdateStrategy.Type == appsv1.OnDeleteDaemonSetStrategyType {
			findings = append(findings, Finding{
				Namespace: ds.Namespace,
				Name:      ds.Name,
				Kind:      "DaemonSet",
				Rule:      "daemonset-update-strategy",
				Severity:  SeverityMedium,
				Message: fmt.Sprintf(
					"DaemonSet %q uses updateStrategy=OnDelete. Kubernetes will NOT automatically "+
						"roll out spec changes — pods keep running the old version until manually "+
						"deleted. DaemonSets typically run cluster-wide agents (log collectors, "+
						"monitoring, network plugins) — a stale agent on upgraded nodes can cause "+
						"silent gaps in observability or incorrect network behaviour. Switch to "+
						"RollingUpdate to get automatic, controlled rollouts.",
					ds.Name,
				),
				Fix: fmt.Sprintf(
					"kubectl patch daemonset %s -n %s "+
						"--type=json -p='[{\"op\":\"replace\",\"path\":\"/spec/updateStrategy/type\",\"value\":\"RollingUpdate\"}]'\n\n"+
						"Why RollingUpdate: Kubernetes will automatically replace pods one node at a time,\n"+
						"ensuring your agents stay current after every deploy or cluster upgrade.",
					ds.Name, ds.Namespace,
				),
			})
		}
	}
	return findings, nil
}
