package runtime

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/Braidplane/cli/pkg/xerror"
)

var (
	runtimeReference      = regexp.MustCompile(`\{\{\s*(runtime\.[^{}]*?)\s*\}\}`)
	runtimeReferenceStart = regexp.MustCompile(`\{\{\s*runtime\b`)
)

// ValidateOperationReferences validates the interpreted operation, including a JSON statement envelope.
func ValidateOperationReferences(statement string, sink, incremental bool) error {
	var operation any
	if json.Unmarshal([]byte(statement), &operation) != nil {
		operation = statement
	}
	return ValidateRuntimeReferences(operation, false, sink, incremental)
}

// ValidateRuntimeReferences checks authored syntax, never resolved values.
// Dynamic keys and checkpoint fields are checked against their execution context later.
func ValidateRuntimeReferences(value any, expressions, sink, incremental bool) error {
	switch v := value.(type) {
	case LiteralTemplate:
		for _, part := range fragments(v) {
			if !part.literal {
				if err := ValidateRuntimeReferences(part.text, expressions, sink, incremental); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		for key, item := range v {
			if err := ValidateRuntimeReferences(item, expressions, sink, incremental); err != nil {
				return xerror.Wrapf("runtime field %q: %w", key, err)
			}
		}
	case []any:
		for _, item := range v {
			if err := ValidateRuntimeReferences(item, expressions, sink, incremental); err != nil {
				return err
			}
		}
	case string:
		remainder := runtimeReference.ReplaceAllString(v, "")
		if runtimeReferenceStart.MatchString(remainder) || unsupportedExecutionOperation.MatchString(remainder) {
			return invalidDateExpression("malformed runtime placeholder")
		}
		for _, match := range runtimeReference.FindAllStringSubmatch(v, -1) {
			parts := strings.Split(match[1], "|")
			name := strings.TrimSpace(parts[0])
			known := false
			switch name {
			case RunSlugParamName, RunDateParamName, RunDateTimeParamName,
				"runtime.previous_success.exists", "runtime.previous_success.run_slug",
				"runtime.previous_success.started_at", "runtime.previous_success.finished_at":
				known = true
			case "runtime.sink.idempotency_key",
				"runtime.sink.idempotency_class", "runtime.sink.replay_identity":
				if !sink {
					return invalidDateExpression(name + " is available only during sink execution")
				}
				known = true
			default:
				for _, namespace := range []string{EnvNamespace, InputNamespace, SecretNamespace} {
					if key, ok := strings.CutPrefix(name, namespace); ok && runtimeInputKeyPattern.MatchString(key) {
						known = true
					}
				}
				for _, bound := range []string{"runtime.incremental.lower_bound", "runtime.incremental.upper_bound"} {
					if name == bound || strings.HasPrefix(name, bound+".") && runtimeInputKeyPattern.MatchString(strings.TrimPrefix(name, bound+".")) {
						if !incremental {
							return invalidDateExpression(name + " requires incremental source execution")
						}
						known = true
					}
				}
			}
			if !known {
				return invalidDateExpression("unknown runtime parameter " + name)
			}
			if !expressions && len(parts) > 1 {
				return invalidDateExpression(name + ": place expressions in query.parameters or sink.config and use named connector parameters")
			}
			if len(parts) > 1 {
				switch name {
				case RunDateParamName, RunDateTimeParamName, "runtime.previous_success.started_at", "runtime.previous_success.finished_at":
				default:
					if !strings.HasPrefix(name, EnvNamespace) && !strings.HasPrefix(name, InputNamespace) {
						return invalidDateExpression(name + ": operations require a date or datetime source")
					}
				}
				if _, err := applyDateOperations(time.Date(2000, 6, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339), parts[1:], false); err != nil {
					return xerror.Wrapf("runtime parameter %s: %w", name, err)
				}
			}
		}
	}
	return nil
}

// ValidateExecutionAvailability checks context without substituting SQL-bound values.
func ValidateExecutionAvailability(value any, snapshot ExecutionContext) error {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			if err := ValidateExecutionAvailability(item, snapshot); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := ValidateExecutionAvailability(item, snapshot); err != nil {
				return err
			}
		}
	case string, LiteralTemplate:
		_, err := resolveAuthoredText(v, func(expression string) (any, bool, bool, error) {
			name, _, _ := strings.Cut(expression, "|")
			name = strings.TrimSpace(name)
			if (name == RunSlugParamName || strings.HasPrefix(name, "runtime.previous_success.")) && snapshot.RunSlug == "" {
				return nil, true, false, invalidDateExpression(name + " requires pipeline execution")
			}
			return nil, false, false, nil
		})
		return err
	}
	return nil
}
