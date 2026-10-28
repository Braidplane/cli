package runtime

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/xerror"
)

// ExecutionContext contains only safe, pinned execution metadata, never input or secret values.
type ExecutionContext struct {
	RunSlug         string         `json:"run_slug,omitempty"`
	At              time.Time      `json:"at"`
	PreviousSuccess *SuccessfulRun `json:"previous_success,omitempty"`
}

type SuccessfulRun struct {
	RunSlug    string    `json:"run_slug"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type (
	executionContextKey     struct{}
	sinkExecutionKey        struct{}
	incrementalExecutionKey struct{}
)

func WithSinkExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, sinkExecutionKey{}, true)
}

func WithIncrementalExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, incrementalExecutionKey{}, true)
}

func IsSinkExecution(ctx context.Context) bool {
	value, _ := ctx.Value(sinkExecutionKey{}).(bool)
	return value
}

func IsIncrementalExecution(ctx context.Context) bool {
	value, _ := ctx.Value(incrementalExecutionKey{}).(bool)
	return value
}

func WithExecutionContext(ctx context.Context, snapshot ExecutionContext) context.Context {
	return context.WithValue(ctx, executionContextKey{}, snapshot)
}

func ExecutionContextFrom(ctx context.Context) (ExecutionContext, bool) {
	snapshot, ok := ctx.Value(executionContextKey{}).(ExecutionContext)
	return snapshot, ok
}

var unsupportedExecutionOperation = regexp.MustCompile(`\{\{[^{}]*\|[^{}]*\}\}`)

// ResolveExecutionValue substitutes metadata before input and secret resolution so supplied
// values cannot introduce executable expressions. Whole-value references retain null/boolean types.
func ResolveExecutionValue(value any, snapshot ExecutionContext) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			resolved, err := ResolveExecutionValue(item, snapshot)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			resolved, err := ResolveExecutionValue(item, snapshot)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	case string, LiteralTemplate:
		return resolveAuthoredText(v, func(expression string) (any, bool, bool, error) {
			name, _, _ := strings.Cut(expression, "|")
			name = strings.TrimSpace(name)
			if !strings.HasPrefix(name, "runtime.run.") && !strings.HasPrefix(name, "runtime.previous_success.") {
				return nil, false, false, nil
			}
			resolved, err := evaluateExecutionExpression(expression, snapshot, true)
			return resolved, true, false, err
		})
	default:
		return value, nil
	}
}

func ResolvePipelineExecution(pipeline *pipelinespec.Pipeline, snapshot ExecutionContext) error {
	if pipeline == nil {
		return nil
	}
	for i := range pipeline.Steps {
		step := &pipeline.Steps[i]
		maps := []*map[string]any{}
		if step.Query != nil {
			maps = append(maps, &step.Query.Parameters)
		}
		if step.Sink != nil {
			maps = append(maps, &step.Sink.Config)
		}
		for _, field := range maps {
			if *field == nil {
				continue
			}
			resolved, err := ResolveExecutionValue(*field, snapshot)
			if err != nil {
				return stepError(i, step.Slug.String(), "runtime expression", err)
			}
			*field = resolved.(map[string]any)
		}
	}
	return nil
}

func invalidDateExpression(detail string) error {
	return xerror.InvalidInput(xerror.PipelineRun).Detail("runtime date expression: " + detail)
}

// ResolveSinkValue checks metadata availability before a sink claims or dispatches an effect.
func ResolveSinkValue(value any, metadata map[string]any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			next, err := ResolveSinkValue(item, metadata)
			if err != nil {
				return nil, err
			}
			result[key] = next
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			next, err := ResolveSinkValue(item, metadata)
			if err != nil {
				return nil, err
			}
			result[i] = next
		}
		return result, nil
	case string, LiteralTemplate:
		return resolveAuthoredText(v, func(name string) (any, bool, bool, error) {
			name = strings.TrimSpace(name)
			if name != RunSlugParamName && !strings.HasPrefix(name, "runtime.sink.") {
				return nil, false, false, nil
			}
			resolved, ok := metadata[name]
			if !ok {
				return nil, true, false, invalidDateExpression(name + " is unavailable during this sink execution")
			}
			return resolved, true, false, nil
		})
	default:
		return value, nil
	}
}

func evaluateExecutionExpression(expression string, snapshot ExecutionContext, checkRange bool) (any, error) {
	parts := strings.Split(expression, "|")
	if len(parts) > 5 {
		return nil, invalidDateExpression("at most four operations are supported")
	}
	name := strings.TrimSpace(parts[0])
	var value any
	switch name {
	case RunSlugParamName:
		if snapshot.RunSlug == "" {
			return nil, invalidDateExpression(name + " requires pipeline execution")
		}
		value = snapshot.RunSlug
	case RunDateParamName:
		value = snapshot.At.UTC().Format(time.DateOnly)
	case RunDateTimeParamName:
		value = snapshot.At.UTC().Format(time.RFC3339)
	case "runtime.previous_success.exists":
		value = snapshot.PreviousSuccess != nil
	case "runtime.previous_success.run_slug", "runtime.previous_success.started_at", "runtime.previous_success.finished_at":
		if prior := snapshot.PreviousSuccess; prior != nil {
			switch name {
			case "runtime.previous_success.run_slug":
				value = prior.RunSlug
			case "runtime.previous_success.started_at":
				value = prior.StartedAt.UTC().Format(time.RFC3339)
			case "runtime.previous_success.finished_at":
				value = prior.FinishedAt.UTC().Format(time.RFC3339)
			}
		}
	default:
		return nil, invalidDateExpression("unknown execution metadata field")
	}
	if len(parts) == 1 {
		return value, nil
	}
	if name != RunDateParamName && name != RunDateTimeParamName && name != "runtime.previous_success.started_at" && name != "runtime.previous_success.finished_at" {
		return nil, invalidDateExpression("operations require a date or datetime field")
	}
	return applyDateOperations(value, parts[1:], checkRange)
}

func applyDateOperations(value any, operations []string, checkRange bool) (any, error) {
	if len(operations) > 4 {
		return nil, invalidDateExpression("at most four operations are supported")
	}
	text, ok := value.(string)
	if !ok {
		return nil, invalidDateExpression("operations require a non-null date or datetime")
	}
	instant, err := time.Parse(time.RFC3339, text)
	if err != nil {
		instant, err = time.Parse(time.DateOnly, text)
	}
	if err != nil {
		return nil, invalidDateExpression("invalid date or datetime")
	}
	format := time.RFC3339
	if len(text) == len(time.DateOnly) {
		format = time.DateOnly
	}
	instant = instant.UTC()
	if checkRange && (instant.Year() < 1 || instant.Year() > 9999) {
		return nil, invalidDateExpression("result must be within years 0001 through 9999")
	}
	for i, raw := range operations {
		operation, argument, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			return nil, invalidDateExpression("expected offset:DURATION, add_days:N, add_months:N or format:date|datetime|unix")
		}
		argument = strings.TrimSpace(argument)
		switch strings.TrimSpace(operation) {
		case "offset":
			duration, err := time.ParseDuration(argument)
			if err != nil {
				return nil, invalidDateExpression("offset requires a valid fixed duration")
			}
			instant = instant.Add(duration)
		case "add_days", "add_months":
			count, err := strconv.Atoi(argument)
			if err != nil || count < -120000 || count > 120000 {
				return nil, invalidDateExpression("calendar offset must be an integer between -120000 and 120000")
			}
			if strings.TrimSpace(operation) == "add_days" {
				instant = instant.AddDate(0, 0, count)
			} else {
				// Shift from day one so short target months cannot overflow.
				target := instant.AddDate(0, 0, 1-instant.Day()).AddDate(0, count, 0)
				lastDay := target.AddDate(0, 1, -1).Day()
				day := min(instant.Day(), lastDay)
				instant = target.AddDate(0, 0, day-1)
			}
		case "format":
			if i != len(operations)-1 {
				return nil, invalidDateExpression("format must be the final operation")
			}
			switch argument {
			case "date":
				format = time.DateOnly
			case "datetime":
				format = time.RFC3339
			case "unix":
				return instant.Unix(), nil
			default:
				return nil, invalidDateExpression("format must be date, datetime or unix")
			}
		default:
			return nil, invalidDateExpression("unsupported operation")
		}
		if checkRange && (instant.Year() < 1 || instant.Year() > 9999) {
			return nil, invalidDateExpression("result must be within years 0001 through 9999")
		}
	}
	return instant.UTC().Format(format), nil
}

// ValidatePipelineExecution checks expression grammar without consulting a clock or history.
func ValidatePipelineExecution(pipeline *pipelinespec.Pipeline) error {
	if pipeline == nil {
		return nil
	}
	type runtimeField struct {
		name                           string
		value                          map[string]any
		expressions, sink, incremental bool
	}
	for i, step := range pipeline.Steps {
		var fields []runtimeField
		if step.Transform != nil && runtimeReferenceStart.MatchString(step.Transform.SQL) {
			return stepError(i, step.Slug.String(), "transform.sql", invalidDateExpression("runtime parameters are unavailable in transform SQL; bind them in a query step"))
		}
		if step.Query != nil {
			incremental := step.Query.Incremental != nil
			if incremental {
				if err := ValidateRuntimeReferences(step.Query.Incremental.LookbackWindow, false, false, false); err != nil {
					return stepError(i, step.Slug.String(), "query.incremental.lookback_window", err)
				}
			}
			fields = append(fields,
				runtimeField{"query.parameters", step.Query.Parameters, true, false, incremental},
				runtimeField{"query.operation_inline", step.Query.OperationInline, false, false, incremental})
		}
		if step.Sink != nil {
			fields = append(fields,
				runtimeField{"sink.config", step.Sink.Config, true, true, false},
				runtimeField{"sink.operation_inline", step.Sink.OperationInline, false, true, false})
		}
		for _, field := range fields {
			if err := ValidateRuntimeReferences(field.value, field.expressions, field.sink, field.incremental); err != nil {
				return stepError(i, step.Slug.String(), field.name, err)
			}
		}
	}
	return nil
}
