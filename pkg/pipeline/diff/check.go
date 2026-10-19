package diff

import (
	"fmt"
	"strings"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
)

//go:generate -command enumer go tool enumer
//go:generate enumer -type=Severity -json -linecomment -output=check_severity_enumer.go

// Severity represents the impact level of a pipeline change.
type Severity int

const (
	// SeveritySafe means the change is additive or cosmetic with no behavioral risk.
	SeveritySafe Severity = iota // safe
	// SeverityWarning means the change alters behavior and warrants careful review.
	SeverityWarning // warning
	// SeverityBreaking means the change removes or incompatibly alters the pipeline contract.
	SeverityBreaking // breaking
)

const (
	boolTextFalse = "false"
	boolTextTrue  = "true"
)

// CheckResult is the overall outcome of a semantic check against a DiffResult.
type CheckResult struct {
	Severity Severity  `json:"severity"`
	Findings []Finding `json:"findings"`
}

// Finding is a single observed issue with a severity and human-readable message.
type Finding struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Check evaluates the semantic impact of a DiffResult and returns a structured result.
// It classifies each change by severity so callers can decide whether to block (e.g. in CI).
func Check(d *DiffResult) *CheckResult {
	r := &CheckResult{Severity: SeveritySafe}

	if !d.HasChanges {
		return r
	}

	add := func(sev Severity, msg string) {
		r.Findings = append(r.Findings, Finding{Severity: sev, Message: msg})
		r.Severity = higherSeverity(r.Severity, sev)
	}

	for _, fc := range d.Metadata {
		switch fc.Field {
		case "metadata.slug":
			add(SeverityBreaking, fmt.Sprintf(
				"pipeline slug changed: %q -> %q (downstream references will break)",
				fc.Before, fc.After,
			))
		case "metadata.name":
			add(SeveritySafe, fmt.Sprintf("pipeline name changed: %q -> %q", fc.Before, fc.After))
		}
	}

	for _, sd := range d.Steps {
		switch sd.Kind {
		case StepRemoved:
			add(SeverityBreaking, fmt.Sprintf("step %q was removed", sd.Slug))
		case StepAdded:
			add(SeveritySafe, fmt.Sprintf("step %q was added", sd.Slug))
		case StepModified:
			for _, fc := range sd.Fields {
				add(severityForStepChange(fc), fmt.Sprintf(
					"%s changed: %q -> %q", fc.Field, fc.Before, fc.After,
				))
			}
		}
	}

	return r
}

// ShouldBlockDiff decides whether a semantic diff should fail preflight/CI.
// Breaking semantic changes block because the current pipeline contract would no longer hold.
func ShouldBlockDiff(after *pipelinespec.Pipeline, result *CheckResult) bool {
	if result == nil {
		return false
	}

	return result.Severity == SeverityBreaking
}

// Format returns a human-readable text summary suitable for CI logs or PR comments.
func (r *CheckResult) Format() string {
	if len(r.Findings) == 0 {
		return "severity: safe\nno changes detected"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "severity: %s\nfindings:\n", r.Severity)
	for _, f := range r.Findings {
		fmt.Fprintf(&sb, "  [%-8s] %s\n", f.Severity, f.Message)
	}
	return sb.String()
}

// severityForStepChange classifies the impact of a single modified step field.
func severityForStepChange(change FieldChange) Severity {
	field := change.Field
	if strings.HasSuffix(field, ".input.output_contract_ref") || strings.HasSuffix(field, ".contract_ref") {
		return SeverityBreaking
	}
	if strings.Contains(field, ".kind") ||
		strings.HasSuffix(field, ".input") ||
		strings.HasSuffix(field, ".query") ||
		strings.HasSuffix(field, ".transform") ||
		strings.HasSuffix(field, ".sink") {
		return SeverityBreaking
	}
	if strings.Contains(field, ".input.output_contract.") ||
		strings.Contains(field, ".transform.input_contracts.") ||
		strings.Contains(field, ".transform.output_contract.") ||
		strings.Contains(field, ".transform.input_contract.") ||
		strings.Contains(field, ".query.output_contract.") ||
		strings.Contains(field, ".sink.input_contract.") {
		if strings.HasSuffix(field, ".representation") ||
			strings.HasSuffix(field, ".root_shape") {
			return SeverityBreaking
		}
		if strings.Contains(field, ".sink.input_contract.") {
			switch {
			case change.After == absentValue:
				return SeverityWarning
			case change.Before == absentValue:
				if strings.Contains(change.After, "(required=true,") {
					return SeverityBreaking
				}
				return SeverityWarning
			case strings.HasSuffix(field, ".nullable"):
				if change.Before == boolTextTrue && change.After == boolTextFalse {
					return SeverityBreaking
				}
				return SeverityWarning
			}
		}
		if change.After == absentValue {
			return SeverityBreaking
		}
		if change.Before == absentValue {
			return SeverityWarning
		}
		if strings.HasSuffix(field, ".type") {
			return SeverityBreaking
		}
		if strings.HasSuffix(field, ".required") {
			if change.Before == boolTextFalse && change.After == boolTextTrue {
				return SeverityBreaking
			}
			return SeverityWarning
		}
		if strings.HasSuffix(field, ".nullable") {
			if change.Before == boolTextFalse && change.After == boolTextTrue {
				return SeverityBreaking
			}
			return SeverityWarning
		}
		if strings.HasSuffix(field, ".output_contract") {
			return SeverityWarning
		}
		if strings.HasSuffix(field, ".input_contract") {
			return SeverityWarning
		}
		if strings.HasSuffix(field, ".query.output_contract") {
			return SeverityWarning
		}
		if strings.Contains(field, ".input.output_contract.") ||
			strings.Contains(field, ".transform.input_contracts.") ||
			strings.Contains(field, ".transform.output_contract.") ||
			strings.Contains(field, ".transform.input_contract.") ||
			strings.Contains(field, ".query.output_contract.") ||
			strings.Contains(field, ".sink.input_contract.") {
			return SeverityWarning
		}
	}
	if strings.Contains(field, ".transform.output_kind") ||
		strings.Contains(field, ".sink.data_source_ref") ||
		strings.Contains(field, ".sink.operation_type") ||
		strings.Contains(field, ".sink.operation_ref") ||
		strings.Contains(field, ".sink.operation_inline") ||
		strings.Contains(field, ".sink.config.") ||
		strings.Contains(field, ".transform.sql") ||
		strings.Contains(field, "operation_type") ||
		strings.Contains(field, "operation_ref") ||
		strings.Contains(field, "operation_inline") ||
		strings.Contains(field, "data_source_ref") {
		return SeverityWarning
	}
	return SeveritySafe
}

// higherSeverity returns whichever severity is more severe (breaking > warning > safe).
func higherSeverity(a, b Severity) Severity {
	rank := map[Severity]int{
		SeveritySafe:     0,
		SeverityWarning:  1,
		SeverityBreaking: 2,
	}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}
