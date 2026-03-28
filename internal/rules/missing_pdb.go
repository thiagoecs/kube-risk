package rules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckMissingPDB flags Deployments and StatefulSets that have no matching
// PodDisruptionBudget. A PDB tells Kubernetes the minimum number of pods that
// must stay up during voluntary disruptions (node drains, upgrades). Without
// one, Kubernetes may evict all pods at once, causing full service outage.
func CheckMissingPDB(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	// Collect all PDB label selectors in the namespace(s)
	pdbs, err := client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing PodDisruptionBudgets: %w", err)
	}

	// Build a set of label-selector strings from existing PDBs so we can
	// quickly check whether a workload's pod template labels are covered.
	pdbSelectors := make([]map[string]string, 0, len(pdbs.Items))
	for _, pdb := range pdbs.Items {
		if pdb.Spec.Selector != nil {
			pdbSelectors = append(pdbSelectors, pdb.Spec.Selector.MatchLabels)
		}
	}

	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		if !isCoveredByPDB(d.Spec.Template.Labels, pdbSelectors) {
			findings = append(findings, Finding{
				Namespace: d.Namespace,
				Name:      d.Name,
				Kind:      "Deployment",
				Rule:      "missing-pdb",
				Severity:  SeverityMedium,
				Message: fmt.Sprintf(
					"Deployment %q has no matching PodDisruptionBudget. Without a PDB, Kubernetes "+
						"can evict all pods simultaneously during node maintenance or cluster upgrades. "+
						"Create a PDB with minAvailable or maxUnavailable to protect this workload.",
					d.Name,
				),
				Fix: pdbFix(d.Name, d.Namespace, d.Spec.Template.Labels),
			})
		}
	}

	// Check StatefulSets
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	for _, ss := range statefulsets.Items {
		if !isCoveredByPDB(ss.Spec.Template.Labels, pdbSelectors) {
			findings = append(findings, Finding{
				Namespace: ss.Namespace,
				Name:      ss.Name,
				Kind:      "StatefulSet",
				Rule:      "missing-pdb",
				Severity:  SeverityMedium,
				Message: fmt.Sprintf(
					"StatefulSet %q has no matching PodDisruptionBudget. StatefulSets often run "+
						"stateful apps like databases — losing all pods at once can corrupt data or "+
						"require manual recovery. Add a PDB to prevent simultaneous eviction.",
					ss.Name,
				),
				Fix: pdbFix(ss.Name, ss.Namespace, ss.Spec.Template.Labels),
			})
		}
	}

	return findings, nil
}

// isCoveredByPDB returns true if workloadLabels are a superset of any PDB's matchLabels.
// This mirrors how Kubernetes label selectors work: a PDB matches a pod if all
// of the PDB's matchLabels appear in the pod's labels.
func isCoveredByPDB(workloadLabels map[string]string, pdbSelectors []map[string]string) bool {
	for _, sel := range pdbSelectors {
		if labelsMatch(workloadLabels, sel) {
			return true
		}
	}
	return false
}

func labelsMatch(podLabels, selectorLabels map[string]string) bool {
	for k, v := range selectorLabels {
		if podLabels[k] != v {
			return false
		}
	}
	return true
}

// pdbFix generates a ready-to-apply PodDisruptionBudget manifest using the
// workload's actual pod template labels. We sort the keys so the output is
// deterministic across runs.
func pdbFix(name, namespace string, labels map[string]string) string {
	// Sort label keys for deterministic output
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var labelLines strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&labelLines, "        %s: %s\n", k, labels[k])
	}

	return fmt.Sprintf(
		"# Save as %s-pdb.yaml and run: kubectl apply -f %s-pdb.yaml\n"+
			"apiVersion: policy/v1\n"+
			"kind: PodDisruptionBudget\n"+
			"metadata:\n"+
			"  name: %s-pdb\n"+
			"  namespace: %s\n"+
			"spec:\n"+
			"  minAvailable: 1\n"+
			"  selector:\n"+
			"    matchLabels:\n"+
			"%s\n"+
			"Why minAvailable: 1 — at least one pod stays running during any node\n"+
			"drain, regardless of replica count. Raise it if you need higher guarantees.",
		name, name, name, namespace, strings.TrimRight(labelLines.String(), "\n"),
	)
}
