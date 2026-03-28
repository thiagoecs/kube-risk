package rules

import "testing"

func TestApplyScores(t *testing.T) {
	t.Run("known rules get correct base scores", func(t *testing.T) {
		cases := []struct {
			rule     string
			severity Severity
			want     int
		}{
			{"single-replica", SeverityHigh, 9},
			{"risky-statefulset", SeverityHigh, 8},
			{"missing-readiness-probe", SeverityHigh, 7},
			{"missing-pdb", SeverityMedium, 5},
			{"risky-statefulset", SeverityMedium, 4},
			{"unsafe-rollout", SeverityMedium, 4},
		}
		for _, c := range cases {
			findings := []Finding{{
				Rule:      c.rule,
				Severity:  c.severity,
				Namespace: "default", // neutral namespace — no boost
			}}
			ApplyScores(findings, "production")
			if findings[0].Score != c.want {
				t.Errorf("rule=%q severity=%s: want score %d, got %d",
					c.rule, c.severity, c.want, findings[0].Score)
			}
		}
	})

	t.Run("prod namespace gets +2 boost in production mode", func(t *testing.T) {
		findings := []Finding{{Rule: "single-replica", Severity: SeverityHigh, Namespace: "production"}}
		ApplyScores(findings, "production")
		if findings[0].Score != 10 { // 9 base + 2 boost, clamped to 10
			t.Errorf("want score 10 for prod namespace, got %d", findings[0].Score)
		}
	})

	t.Run("live namespace gets +2 boost in production mode", func(t *testing.T) {
		findings := []Finding{{Rule: "single-replica", Severity: SeverityHigh, Namespace: "live"}}
		ApplyScores(findings, "production")
		if findings[0].Score != 10 {
			t.Errorf("want score 10 for live namespace, got %d", findings[0].Score)
		}
	})

	t.Run("dev namespace gets -1 in production mode", func(t *testing.T) {
		findings := []Finding{{Rule: "missing-pdb", Severity: SeverityMedium, Namespace: "dev"}}
		ApplyScores(findings, "production")
		if findings[0].Score != 4 { // 5 base - 1
			t.Errorf("want score 4 for dev namespace, got %d", findings[0].Score)
		}
	})

	t.Run("namespace boost is skipped in development mode", func(t *testing.T) {
		// Same prod namespace, but development mode — no boost applied.
		findings := []Finding{{Rule: "missing-readiness-probe", Severity: SeverityHigh, Namespace: "production"}}
		ApplyScores(findings, "development")
		if findings[0].Score != 7 { // base score only, no +2
			t.Errorf("want score 7 in dev mode (no boost), got %d", findings[0].Score)
		}
	})

	t.Run("score is clamped to minimum of 1", func(t *testing.T) {
		// Unknown rule in a dev namespace: fallback LOW base=2, -1 boost = 1 (clamped)
		findings := []Finding{{Rule: "unknown-rule", Severity: SeverityLow, Namespace: "staging"}}
		ApplyScores(findings, "production")
		if findings[0].Score < 1 {
			t.Errorf("score should never be below 1, got %d", findings[0].Score)
		}
	})

	t.Run("score is clamped to maximum of 10", func(t *testing.T) {
		findings := []Finding{{Rule: "single-replica", Severity: SeverityHigh, Namespace: "prod-live"}}
		ApplyScores(findings, "production")
		if findings[0].Score > 10 {
			t.Errorf("score should never exceed 10, got %d", findings[0].Score)
		}
	})
}

func TestNamespaceBoost(t *testing.T) {
	tests := []struct {
		namespace string
		want      int
	}{
		{"production", +2},
		{"prod", +2},
		{"my-prod-cluster", +2},
		{"live", +2},
		{"live-eu", +2},
		{"dev", -1},
		{"development", -1},
		{"staging", -1},
		{"test", -1},
		{"testing", -1},
		{"default", 0},
		{"backend", 0},
		{"payments", 0},
	}
	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			if got := namespaceBoost(tt.namespace); got != tt.want {
				t.Errorf("namespaceBoost(%q) = %d, want %d", tt.namespace, got, tt.want)
			}
		})
	}
}
