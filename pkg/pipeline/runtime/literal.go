package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
)

// LiteralLocation records interpretation metadata without copying supplied values.
type LiteralLocation struct {
	Path   []string `json:"path"`
	Ranges [][2]int `json:"ranges,omitempty"`
}

// LiteralTemplate preserves supplied spans while other authored references await resolution.
type LiteralTemplate struct {
	Text   string
	Ranges [][2]int
}

// IncrementalValue defers an authored reference until the source checkpoint is known.
type IncrementalValue struct{ Template any }

func (v LiteralTemplate) MarshalJSON() ([]byte, error) { return json.Marshal(v.Text) }

type textFragment struct {
	text    string
	literal bool
}

func fragments(value any) []textFragment {
	switch v := value.(type) {
	case LiteralString:
		return []textFragment{{string(v), true}}
	case LiteralTemplate:
		var result []textFragment
		last := 0
		for _, span := range v.Ranges {
			result = append(result, textFragment{v.Text[last:span[0]], false}, textFragment{v.Text[span[0]:span[1]], true})
			last = span[1]
		}
		return append(result, textFragment{v.Text[last:], false})
	case string:
		return []textFragment{{v, false}}
	default:
		return nil
	}
}

func combineFragments(parts []textFragment) any {
	var text strings.Builder
	var ranges [][2]int
	for _, part := range parts {
		start := text.Len()
		text.WriteString(part.text)
		if part.literal && text.Len() > start {
			if len(ranges) > 0 && ranges[len(ranges)-1][1] == start {
				ranges[len(ranges)-1][1] = text.Len()
			} else {
				ranges = append(ranges, [2]int{start, text.Len()})
			}
		}
	}
	if len(ranges) == 0 {
		return text.String()
	}
	if len(ranges) == 1 && ranges[0] == [2]int{0, text.Len()} {
		return LiteralString(text.String())
	}
	return LiteralTemplate{Text: text.String(), Ranges: ranges}
}

func validLiteralRanges(text string, ranges [][2]int) bool {
	last := 0
	for _, span := range ranges {
		if span[0] < last || span[1] <= span[0] || span[1] > len(text) || !utf8.ValidString(text[last:span[0]]) || !utf8.ValidString(text[span[0]:span[1]]) {
			return false
		}
		last = span[1]
	}
	return utf8.ValidString(text[last:])
}

// resolveAuthoredText visits only authored spans. Supplied values are never parsed again.
func resolveAuthoredText(value any, resolve func(string) (any, bool, bool, error)) (any, error) {
	parts := fragments(value)
	var result []textFragment
	for _, part := range parts {
		if part.literal {
			result = append(result, part)
			continue
		}
		last := 0
		for _, match := range runtimeReference.FindAllStringSubmatchIndex(part.text, -1) {
			resolved, found, supplied, err := resolve(part.text[match[2]:match[3]])
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			if len(parts) == 1 && match[0] == 0 && match[1] == len(part.text) {
				return resolved, nil
			}
			if resolved == nil {
				return nil, invalidDateExpression("null cannot be interpolated; use a whole-value reference")
			}
			result = append(result, textFragment{part.text[last:match[0]], false})
			text := runtimeInputSource(resolved)
			result = append(result, textFragment{text, supplied && literalRuntimeValue(text)})
			last = match[1]
		}
		result = append(result, textFragment{part.text[last:], false})
	}
	return combineFragments(result), nil
}

type literalBindingsKey struct{}

func WithLiteralBindings(ctx context.Context, values map[string]any) context.Context {
	return context.WithValue(ctx, literalBindingsKey{}, values)
}

func LiteralBindingsFrom(ctx context.Context) map[string]any {
	values, _ := ctx.Value(literalBindingsKey{}).(map[string]any)
	return values
}

// FreezeLiteralOperationFields turns supplied spans into ordinary connector bindings.
// Their request-local values are inserted by connectors once, after authored syntax validation.
func FreezeLiteralOperationFields(pipeline *pipelinespec.Pipeline) map[slug.Slug]map[string]any {
	byStep := make(map[slug.Slug]map[string]any)
	var bindings map[string]any
	var freeze func(any) any
	freeze = func(value any) any {
		switch v := value.(type) {
		case LiteralString, LiteralTemplate:
			var text strings.Builder
			for _, part := range fragments(v) {
				if part.literal {
					name := fmt.Sprintf("runtime.literal.value_%d", len(bindings))
					bindings[name] = part.text
					text.WriteString("{{" + name + "}}")
				} else {
					text.WriteString(part.text)
				}
			}
			return text.String()
		case map[string]any:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				v[key] = freeze(v[key])
			}
		case []any:
			for i, item := range v {
				v[i] = freeze(item)
			}
		}
		return value
	}
	for i := range pipeline.Steps {
		step := &pipeline.Steps[i]
		bindings = make(map[string]any)
		if step.Query != nil {
			freeze(step.Query.OperationInline)
			finalizeParameterLiterals(step.Query.Parameters)
		}
		if step.Sink != nil {
			keys := make([]string, 0, len(step.Sink.Config))
			for key := range step.Sink.Config {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if !strings.HasPrefix(key, EnvNamespace) && !strings.HasPrefix(key, InputNamespace) && !strings.HasPrefix(key, SecretNamespace) {
					step.Sink.Config[key] = freeze(step.Sink.Config[key])
				}
			}
			freeze(step.Sink.OperationInline)
		}
		byStep[step.Slug] = bindings
	}
	return byStep
}

// LiteralValue removes interpretation markers from values already bound as data.
func LiteralValue(value any) any {
	switch v := value.(type) {
	case LiteralString:
		return string(v)
	case LiteralTemplate:
		return v.Text
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = LiteralValue(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = LiteralValue(item)
		}
		return result
	default:
		return value
	}
}

// BoundOperationIdentity materializes dynamic bindings only for checkpoint hashing.
// Dispatch keeps the original template so SQL and connector values remain bound data.
func BoundOperationIdentity(value any, parameters map[string]any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			next, err := BoundOperationIdentity(item, parameters)
			if err != nil {
				return nil, err
			}
			result[key] = next
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			next, err := BoundOperationIdentity(item, parameters)
			if err != nil {
				return nil, err
			}
			result[i] = next
		}
		return result, nil
	case string, LiteralTemplate:
		return resolveAuthoredText(v, func(name string) (any, bool, bool, error) {
			name = strings.TrimSpace(name)
			if !strings.HasPrefix(name, EnvNamespace) && !strings.HasPrefix(name, InputNamespace) && !strings.HasPrefix(name, SecretNamespace) {
				return nil, false, false, nil
			}
			resolved, ok := parameters[name]
			if !ok {
				return nil, true, true, invalidDateExpression(name + " is unavailable for checkpoint identity")
			}
			return resolved, true, true, nil
		})
	default:
		return value, nil
	}
}

func finalizeParameterLiterals(values map[string]any) {
	for key, value := range values {
		if hasNamespaceReferenceInValue(value, "runtime.incremental.") {
			switch value.(type) {
			case string, LiteralTemplate:
				values[key] = IncrementalValue{Template: value}
				continue
			}
		}
		switch v := value.(type) {
		case LiteralTemplate:
			values[key] = LiteralString(v.Text)
		case map[string]any:
			finalizeParameterLiterals(v)
		case []any:
			for i, item := range v {
				m := map[string]any{"value": item}
				finalizeParameterLiterals(m)
				v[i] = m["value"]
			}
		}
	}
}

func ResolveIncrementalValue(value any, parameters map[string]any) (any, error) {
	switch v := value.(type) {
	case IncrementalValue:
		resolved, err := resolveAuthoredText(v.Template, func(name string) (any, bool, bool, error) {
			name = strings.TrimSpace(name)
			if !strings.HasPrefix(name, "runtime.incremental.") {
				return nil, false, false, nil
			}
			bound, ok := parameters[name]
			if !ok {
				return nil, true, true, invalidDateExpression(name + " is unavailable in this source checkpoint")
			}
			return bound, true, true, nil
		})
		return LiteralValue(resolved), err
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			next, err := ResolveIncrementalValue(item, parameters)
			if err != nil {
				return nil, err
			}
			result[key] = next
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			next, err := ResolveIncrementalValue(item, parameters)
			if err != nil {
				return nil, err
			}
			result[i] = next
		}
		return result, nil
	default:
		return LiteralValue(value), nil
	}
}
