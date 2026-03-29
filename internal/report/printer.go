package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/thiagomcp/kube-risk/internal/rules"
)

var severityOrder = map[rules.Severity]int{
	rules.SeverityHigh:   0,
	rules.SeverityMedium: 1,
	rules.SeverityLow:    2,
}

var severityIcon = map[rules.Severity]string{
	rules.SeverityHigh:   "🔴 HIGH  ",
	rules.SeverityMedium: "🟡 MEDIUM",
	rules.SeverityLow:    "🟢 LOW   ",
}

// Options controls report rendering behaviour.
type Options struct {
	// Environment is "production" (default) or "development".
	// Affects the report header and which rules were skipped.
	Environment string
}

// Print writes a human-readable risk report to w.
func Print(w io.Writer, findings []rules.Finding, opts Options) {
	env := opts.Environment
	if env == "" {
		env = "production"
	}

	if len(findings) == 0 {
		fmt.Fprintln(w, "✅  No risks found. Your cluster looks healthy!")
		if env == "development" {
			fmt.Fprintln(w, "    (scale rules skipped — run with --environment production for a full scan)")
		}
		return
	}

	sorted := sortFindings(findings)
	high, medium, low := countBySeverity(sorted)

	// ── Header ────────────────────────────────────────────────────────────────
	fmt.Fprintln(w, strings.Repeat("─", 72))
	if env == "development" {
		fmt.Fprintf(w, "  KUBE-RISK REPORT (development)   %d findings  [HIGH: %d  MEDIUM: %d  LOW: %d]\n",
			len(sorted), high, medium, low)
		fmt.Fprintln(w, "  Showing config quality issues that will carry into production if not fixed.")
		fmt.Fprintln(w, "  Scale rules skipped (single-replica, missing-pdb) — expected to differ in dev.")
	} else {
		fmt.Fprintf(w, "  KUBE-RISK REPORT (production)   %d findings  [HIGH: %d  MEDIUM: %d  LOW: %d]\n",
			len(sorted), high, medium, low)
	}
	fmt.Fprintln(w, strings.Repeat("─", 72))

	// ── Findings list ─────────────────────────────────────────────────────────
	for i, f := range sorted {
		icon := severityIcon[f.Severity]
		fmt.Fprintf(w, "\n[%d] %s  %s/%s (%s)   score: %d/10\n",
			i+1, icon, f.Namespace, f.Name, f.Kind, f.Score)
		fmt.Fprintf(w, "    Rule: %s\n", f.Rule)
		fmt.Fprintln(w, wordWrap(f.Message, 68, "    "))
		if f.Fix != "" {
			printFix(w, f.Fix, "    ")
		}
	}

	// ── Workload summary ──────────────────────────────────────────────────────
	printWorkloadSummary(w, sorted)

	// ── Fix this first ────────────────────────────────────────────────────────
	printFixThisFirst(w, sorted)

	// ── Footer ────────────────────────────────────────────────────────────────
	fmt.Fprintln(w, strings.Repeat("─", 72))
	fmt.Fprintf(w, "  %d risk(s) found. Address HIGH findings before cluster upgrades.\n", len(sorted))
	fmt.Fprintln(w, strings.Repeat("─", 72))
}

// printWorkloadSummary groups findings by workload and prints total risk scores,
// sorted highest score first. This lets operators see at a glance which
// workload needs the most attention.
func printWorkloadSummary(w io.Writer, findings []rules.Finding) {
	type workloadKey struct {
		Namespace, Name, Kind string
	}

	totals := map[workloadKey]int{}
	order := []workloadKey{} // preserve first-seen order, then re-sort

	for _, f := range findings {
		k := workloadKey{f.Namespace, f.Name, f.Kind}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		totals[k] += f.Score
	}

	// Sort by total score descending
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && totals[order[j]] > totals[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}

	fmt.Fprintf(w, "\n%s\n", strings.Repeat("─", 72))
	fmt.Fprintln(w, "  WORKLOAD RISK SUMMARY   (total score across all findings)")
	fmt.Fprintln(w, strings.Repeat("─", 72))
	for _, k := range order {
		total := totals[k]
		bar := riskBar(total, 30)
		fmt.Fprintf(w, "  %-40s  %s  %d pts\n",
			k.Namespace+"/"+k.Name+" ("+k.Kind+")", bar, total)
	}
}

// printFixThisFirst picks the top 3 findings by score and explains why they
// should be addressed before anything else.
func printFixThisFirst(w io.Writer, findings []rules.Finding) {
	// Sort a copy purely by score descending to find the top 3
	byScore := make([]rules.Finding, len(findings))
	copy(byScore, findings)
	for i := 1; i < len(byScore); i++ {
		for j := i; j > 0 && byScore[j].Score > byScore[j-1].Score; j-- {
			byScore[j], byScore[j-1] = byScore[j-1], byScore[j]
		}
	}

	top := byScore
	if len(top) > 3 {
		top = top[:3]
	}

	fmt.Fprintf(w, "\n%s\n", strings.Repeat("─", 72))
	fmt.Fprintln(w, "  FIX THIS FIRST")
	fmt.Fprintln(w, strings.Repeat("─", 72))
	for i, f := range top {
		icon := severityIcon[f.Severity]
		fmt.Fprintf(w, "\n  #%d  %s  %s/%s (%s)   score: %d/10\n",
			i+1, icon, f.Namespace, f.Name, f.Kind, f.Score)
		fmt.Fprintln(w, wordWrap(whyItMatters(f), 68, "      "))
		if f.Fix != "" {
			printFix(w, f.Fix, "      ")
		}
	}
	fmt.Fprintln(w)
}

// whyItMatters returns a short, punchy rationale for why this specific finding
// should be prioritized. Different from the full Message — this is a tiebreaker
// argument aimed at someone deciding where to spend the next 30 minutes.
func whyItMatters(f rules.Finding) string {
	reasons := map[string]string{
		"single-replica": "Every cluster upgrade drains nodes one at a time. With a single " +
			"replica, that drain = downtime. There is no fallback pod. Fix this " +
			"first because it affects every future upgrade and every node failure.",
		"risky-statefulset": func() string {
			if f.Severity == rules.SeverityHigh {
				return "OnDelete means your next kubectl apply will silently do nothing. " +
					"Operators often discover this after assuming a fix was deployed. " +
					"Switch to RollingUpdate so changes actually take effect."
			}
			return "Parallel startup removes ordering guarantees. For databases or " +
				"leader-election apps, this can cause split-brain on restart. " +
				"Confirm your app handles concurrent startup before keeping this."
		}(),
		"missing-readiness-probe": "Without a readiness probe, Kubernetes sends live traffic " +
			"to your pod the moment the container starts — before your app has " +
			"finished initializing. Every rolling update currently causes a brief " +
			"window of errors for real users.",
		"missing-pdb": "No PodDisruptionBudget means a node drain can evict all your pods " +
			"simultaneously. A PDB is a one-time, low-effort fix that protects " +
			"every future upgrade automatically.",
		"unsafe-rollout": "This deployment can take down the majority of its capacity in a " +
			"single step. If the new version has a bug, most of your traffic is " +
			"already affected before Kubernetes can roll back.",
		"missing-liveness-probe": "A deadlocked process stays Running forever — Kubernetes " +
			"never restarts it. During an upgrade, stuck pods block drain completion " +
			"and leave broken instances serving traffic indefinitely.",
		"hpa-min-replicas": "This is a hidden trap: you may have set replicas=2 and a PDB, " +
			"but the HPA quietly scales you back to 1 during quiet periods. The next " +
			"node drain finds a single pod and takes it down. Fix the HPA first — " +
			"otherwise all other replica fixes are only effective when traffic is high.",
		"missing-resources": "Without resource limits, one misbehaving pod can consume all " +
			"CPU and memory on a node during an upgrade, triggering OOM kills of " +
			"neighbouring pods. Without requests, the scheduler may place pods on " +
			"nodes that can't actually support them.",
		"latest-image-tag": "An unpinned image means rescheduled pods (which happen on every " +
			"node drain) may start a different version than what you originally deployed. " +
			"Rollback becomes impossible — you can't redeploy the old image if you " +
			"don't know what it was.",
		"daemonset-update-strategy": "OnDelete on a DaemonSet means your monitoring agents, " +
			"log collectors, or network plugins silently run stale code after every " +
			"spec change. On upgraded nodes this can cause gaps in observability or " +
			"incorrect behaviour that's very hard to diagnose.",
	}

	if reason, ok := reasons[f.Rule]; ok {
		ns := strings.ToLower(f.Namespace)
		if strings.Contains(ns, "prod") || strings.Contains(ns, "live") {
			reason += " This workload is in a production namespace — the impact is on live users."
		}
		return reason
	}
	return f.Message
}

// riskBar renders a simple ASCII progress bar proportional to the score.
// maxScore is the maximum possible total score (used for scaling).
func riskBar(score, width int) string {
	// We scale relative to a "bad" workload with ~20 total points (e.g. 2–3 HIGH findings)
	const referenceMax = 20
	filled := score * width / referenceMax
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// ── Sorting ───────────────────────────────────────────────────────────────────

func countBySeverity(findings []rules.Finding) (high, medium, low int) {
	for _, f := range findings {
		switch f.Severity {
		case rules.SeverityHigh:
			high++
		case rules.SeverityMedium:
			medium++
		case rules.SeverityLow:
			low++
		}
	}
	return
}

// sortFindings sorts by severity first (HIGH→MEDIUM→LOW), then by score
// descending within the same severity so the most impactful findings appear
// at the top of each group.
func sortFindings(findings []rules.Finding) []rules.Finding {
	out := make([]rules.Finding, len(findings))
	copy(out, findings)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && less(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func less(a, b rules.Finding) bool {
	ao := severityOrder[a.Severity]
	bo := severityOrder[b.Severity]
	if ao != bo {
		return ao < bo // HIGH before MEDIUM before LOW
	}
	return a.Score > b.Score // higher score first within the same severity
}

// printFix renders a fix block with a clear header so it stands out from the
// finding description. indent is the base indentation string for the block.
func printFix(w io.Writer, fix, indent string) {
	// Total line width is 72 chars. Subtract indent and box chrome to get fill width.
	width := 72 - len(indent)
	header := "─ Suggested fix "
	fill := strings.Repeat("─", width-len(header)-2) // 2 for ┌ and space
	fmt.Fprintf(w, "\n%s┌%s%s\n", indent, header, fill)
	for _, line := range strings.Split(fix, "\n") {
		fmt.Fprintf(w, "%s│  %s\n", indent, line)
	}
	fmt.Fprintf(w, "%s└%s\n", indent, strings.Repeat("─", width-1))
}

// wordWrap wraps text at maxWidth characters, preserving indent on each line.
func wordWrap(text string, maxWidth int, indent string) string {
	words := strings.Fields(text)
	var lines []string
	line := indent

	for _, word := range words {
		if len(line)+len(word)+1 > maxWidth+len(indent) && line != indent {
			lines = append(lines, line)
			line = indent + word
		} else {
			if line == indent {
				line += word
			} else {
				line += " " + word
			}
		}
	}
	if line != indent {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
