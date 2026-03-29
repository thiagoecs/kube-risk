package rules

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckLatestImageTag flags Deployments, StatefulSets, and DaemonSets that use
// the "latest" image tag or no tag at all. During a cluster upgrade, nodes are
// drained and pods rescheduled — if the image has changed since the pod last
// started, the new pod runs different code than the old one, making rollback
// impossible and behaviour unpredictable.
func CheckLatestImageTag(ctx context.Context, client kubernetes.Interface, namespace string) ([]Finding, error) {
	var findings []Finding

	// Check Deployments
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	for _, d := range deployments.Items {
		for _, c := range d.Spec.Template.Spec.Containers {
			if isLatestTag(c.Image) {
				findings = append(findings, Finding{
					Namespace: d.Namespace,
					Name:      d.Name,
					Kind:      "Deployment",
					Rule:      "latest-image-tag",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in Deployment %q uses image %q. The \"latest\" tag (or no tag) is "+
							"mutable — it can silently point to a different image on every pull. During a "+
							"cluster upgrade, rescheduled pods may start a different version than what was "+
							"originally deployed, making rollbacks unreliable. Pin to an immutable tag "+
							"(e.g. a SHA256 digest or a semantic version like nginx:1.25.3).",
						c.Name, d.Name, c.Image,
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
			if isLatestTag(c.Image) {
				findings = append(findings, Finding{
					Namespace: ss.Namespace,
					Name:      ss.Name,
					Kind:      "StatefulSet",
					Rule:      "latest-image-tag",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in StatefulSet %q uses image %q. For stateful workloads, "+
							"unpinned images are especially dangerous — an accidental major version pull "+
							"(e.g. postgres:latest jumping from 15 to 16) can break data compatibility "+
							"and require manual migration.",
						c.Name, ss.Name, c.Image,
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
			if isLatestTag(c.Image) {
				findings = append(findings, Finding{
					Namespace: ds.Namespace,
					Name:      ds.Name,
					Kind:      "DaemonSet",
					Rule:      "latest-image-tag",
					Severity:  SeverityMedium,
					Message: fmt.Sprintf(
						"Container %q in DaemonSet %q uses image %q. DaemonSets run on every node — "+
							"an unpinned image means every new node that joins the cluster (e.g. during "+
							"an upgrade scale-out) may pull a different version.",
						c.Name, ds.Name, c.Image,
					),
				})
				break
			}
		}
	}

	return findings, nil
}

// isLatestTag returns true if the image uses the "latest" tag or has no tag.
// Examples that return true:  "nginx", "nginx:latest", "registry.io/nginx:latest"
// Examples that return false: "nginx:1.25.3", "nginx@sha256:abc123..."
func isLatestTag(image string) bool {
	// Strip registry prefix if present (contains a dot or colon before the first slash)
	name := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		name = image[i+1:]
	}
	// Image digest — always immutable
	if strings.Contains(image, "@sha256:") {
		return false
	}
	// No colon in the name portion means no tag — defaults to latest
	if !strings.Contains(name, ":") {
		return true
	}
	// Explicit :latest tag
	parts := strings.SplitN(name, ":", 2)
	return parts[1] == "latest"
}
