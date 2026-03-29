package rules

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// suppressAnnotation is the annotation key used to opt a workload out of
// one or more rules. Value is a comma-separated list of rule names, or "*"
// to suppress all findings for that workload.
//
// Example:
//
//	kube-risk/suppress: "single-replica,missing-pdb"
//	kube-risk/suppress: "*"
//
// Teams should pair it with a reason annotation for auditability:
//
//	kube-risk/suppress-reason: "single replica intentional — this is a read-only cache"
const suppressAnnotation = "kube-risk/suppress"

// filterSuppressed removes findings for workloads that have opted out of
// a specific rule via the kube-risk/suppress annotation.
// Returns the filtered findings and the count of suppressed findings.
func filterSuppressed(ctx context.Context, client kubernetes.Interface, findings []Finding) ([]Finding, int) {
	// Cache annotations per workload to avoid redundant API calls.
	cache := make(map[string]map[string]string)

	var kept []Finding
	suppressed := 0
	for _, f := range findings {
		cacheKey := f.Kind + "/" + f.Namespace + "/" + f.Name
		if _, ok := cache[cacheKey]; !ok {
			cache[cacheKey] = fetchAnnotations(ctx, client, f.Namespace, f.Name, f.Kind)
		}
		if isSuppressed(cache[cacheKey], f.Rule) {
			suppressed++
			continue
		}
		kept = append(kept, f)
	}
	return kept, suppressed
}

func isSuppressed(annotations map[string]string, rule string) bool {
	val, ok := annotations[suppressAnnotation]
	if !ok {
		return false
	}
	for _, r := range strings.Split(val, ",") {
		r = strings.TrimSpace(r)
		if r == "*" || r == rule {
			return true
		}
	}
	return false
}

func fetchAnnotations(ctx context.Context, client kubernetes.Interface, ns, name, kind string) map[string]string {
	switch kind {
	case "Deployment":
		obj, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return obj.Annotations
	case "StatefulSet":
		obj, err := client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return obj.Annotations
	case "DaemonSet":
		obj, err := client.AppsV1().DaemonSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return obj.Annotations
	case "HorizontalPodAutoscaler":
		obj, err := client.AutoscalingV2().HorizontalPodAutoscalers(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return obj.Annotations
	}
	return nil
}
