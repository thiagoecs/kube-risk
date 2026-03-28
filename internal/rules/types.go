package rules

// Severity represents the risk level of a finding.
type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
)

// Finding represents a single risk detected in the cluster.
type Finding struct {
	// Namespace and name of the affected workload
	Namespace string
	Name      string
	// Kind is the workload type, e.g. "Deployment", "StatefulSet"
	Kind string
	// Rule is a short identifier for the rule that fired, e.g. "single-replica"
	Rule string
	// Severity of the risk
	Severity Severity
	// Score is a numeric risk score 1–10. Computed after rules run by ApplyScores.
	// Higher = fix sooner. Accounts for rule type and namespace environment.
	Score int
	// Message is a human-readable explanation of the risk and why it matters
	Message string
}

// Rule is a function that inspects a cluster and returns findings.
// Each rule receives the Kubernetes client and target namespace ("" = all namespaces).
type Rule func(ctx RuleContext) ([]Finding, error)

// RuleContext holds everything a rule needs to do its job.
type RuleContext struct {
	Client    interface{ GetNamespace() string } // replaced by concrete type in runner
	Namespace string
}
