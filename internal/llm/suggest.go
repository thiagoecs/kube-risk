package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/thiagomcp/kube-risk/internal/rules"
)

const model = anthropic.ModelClaudeHaiku4_5

// Suggest calls Claude to apply fixes for unfixable findings directly to the
// manifest. It returns the full patched manifest bytes. The caller should
// treat this as an AI-generated suggestion and include a review note in the PR.
//
// Only findings with an empty Fix field are passed to the LLM — mechanically
// fixable findings are handled by the patcher before this is called.
func Suggest(apiKey string, manifest []byte, findings []rules.Finding) ([]byte, error) {
	unfixable := make([]rules.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Fix == "" {
			unfixable = append(unfixable, f)
		}
	}
	if len(unfixable) == 0 {
		return manifest, nil
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := buildPrompt(manifest, unfixable)

	msg, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("calling Claude API: %w", err)
	}

	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("Claude returned an empty response")
	}

	raw := msg.Content[0].Text
	return extractYAML(raw, manifest), nil
}

func buildPrompt(manifest []byte, findings []rules.Finding) string {
	var sb strings.Builder

	sb.WriteString("You are a Kubernetes reliability expert. ")
	sb.WriteString("Your task is to fix the issues listed below in the Kubernetes manifest provided.\n\n")

	sb.WriteString("## Issues to fix\n\n")
	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", f.Rule, f.Severity, f.Message))
	}

	sb.WriteString("\n## Rules\n\n")
	sb.WriteString("- Return ONLY the complete fixed YAML manifest. No explanation, no markdown code fences, no commentary.\n")
	sb.WriteString("- Preserve all existing fields, comments, labels, and annotations exactly.\n")
	sb.WriteString("- For missing readiness/liveness probes: infer the correct probe type and port from the container's `ports` field and image name. Use httpGet if a port is exposed, tcpSocket as a fallback. Set safe defaults: initialDelaySeconds=10, periodSeconds=10, failureThreshold=3.\n")
	sb.WriteString("- For risky-statefulset OnDelete: change updateStrategy.type to RollingUpdate.\n")
	sb.WriteString("- For risky-statefulset Parallel: add a comment explaining the risk but do NOT change the value — the operator must decide.\n")
	sb.WriteString("- Do not change anything that is not directly related to the listed issues.\n")

	sb.WriteString("\n## Manifest\n\n")
	sb.Write(manifest)

	return sb.String()
}

// extractYAML strips markdown code fences if Claude added them despite the
// instructions, and falls back to the original manifest on empty output.
func extractYAML(response string, original []byte) []byte {
	s := strings.TrimSpace(response)

	// Strip ```yaml ... ``` or ``` ... ``` fences.
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}

	if s == "" {
		return original
	}
	return []byte(s + "\n")
}
