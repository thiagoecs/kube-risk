package patcher

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/thiagomcp/kube-risk/internal/rules"
)

// PatchFile applies YAML fixes to content for the given findings.
// Handles: single-replica (sets spec.replicas=2), unsafe-rollout (sets maxUnavailable=1).
// Returns the patched bytes; if no applicable fixes, content is returned unchanged.
func PatchFile(content []byte, findings []rules.Finding) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	changed := false
	for _, f := range findings {
		switch f.Rule {
		case "single-replica":
			if err := setIntAtPath(&doc, []string{"spec", "replicas"}, 2); err != nil {
				return nil, fmt.Errorf("patching replicas: %w", err)
			}
			changed = true
		case "unsafe-rollout":
			if err := setIntAtPath(&doc, []string{"spec", "strategy", "rollingUpdate", "maxUnavailable"}, 1); err != nil {
				return nil, fmt.Errorf("patching maxUnavailable: %w", err)
			}
			changed = true
		}
	}

	if !changed {
		return content, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // match the 2-space indentation convention used in the sample manifests
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("serializing YAML: %w", err)
	}
	_ = enc.Close()

	out := buf.Bytes()
	// yaml.NewEncoder may prepend "---\n"; strip it if the original didn't have it.
	if !bytes.HasPrefix(content, []byte("---")) {
		out = bytes.TrimPrefix(out, []byte("---\n"))
	}
	return out, nil
}

// ExtractPDBYAML returns the pure YAML portion from a missing-pdb Fix string,
// stripping the human-readable "Why ..." explanation that follows the YAML body.
func ExtractPDBYAML(fix string) string {
	// Try double-newline separator first, then single — be robust to either.
	for _, sep := range []string{"\n\nWhy ", "\nWhy "} {
		if i := strings.Index(fix, sep); i >= 0 {
			return strings.TrimSpace(fix[:i]) + "\n"
		}
	}
	return fix
}

// setIntAtPath navigates the yaml.v3 Node tree by dot-separated path and sets
// the leaf node to the given integer value. Comments are preserved because we
// only mutate the value field of the target node, not the surrounding structure.
func setIntAtPath(doc *yaml.Node, path []string, value int) error {
	node := doc
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	for i, key := range path {
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("expected mapping at path segment %q", key)
		}
		found := false
		for j := 0; j+1 < len(node.Content); j += 2 {
			if node.Content[j].Value == key {
				if i == len(path)-1 {
					node.Content[j+1].Value = strconv.Itoa(value)
					node.Content[j+1].Tag = "!!int"
					return nil
				}
				node = node.Content[j+1]
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("key %q not found", key)
		}
	}
	return nil
}
