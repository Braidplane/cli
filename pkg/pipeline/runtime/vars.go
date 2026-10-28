package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Braidplane/cli/pkg/model"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/xerror"
)

const (
	Namespace            = "runtime."
	EnvNamespace         = Namespace + "env."
	SecretNamespace      = Namespace + "secrets."
	InputNamespace       = Namespace + "input."
	RunSlugParamName     = Namespace + "run.slug"
	RunDateParamName     = Namespace + "run.date"
	RunDateTimeParamName = Namespace + "run.datetime"
	stepReferencePrefix  = "$steps."
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\}\}`)

var runtimeInputKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var secretRuntimeInputKeyFragments = []string{
	"secret",
	"password",
	"passwd",
	"token",
	"api_key",
	"apikey",
	"access_key",
	"accesskey",
	"private_key",
	"privatekey",
	"credential",
	"auth",
}

type resolvedVariable struct {
	value any
	date  bool
}

type variableResolver struct {
	namespace string
	entity    xerror.Entity
	missing   func(string) error
}

var (
	envVariableResolver = variableResolver{
		namespace: EnvNamespace,
		entity:    xerror.RuntimeVariable,
		missing:   missingVariable,
	}
	inputVariableResolver = variableResolver{
		namespace: InputNamespace,
		entity:    xerror.PipelineRun,
		missing:   missingInput,
	}
	secretVariableResolver = variableResolver{
		namespace: SecretNamespace,
		entity:    xerror.EnvironmentSecret,
		missing:   missingSecret,
	}
)

// LiteralString is a connector literal produced from a whole-value text runtime
// variable that would otherwise collide with a later runtime expression
// language. It is unwrapped back to string before connector execution.
type LiteralString string

// ResolvePipeline substitutes product-managed runtime variables in environment-aware
// runtime fields. Non-env placeholders are left for connector/query execution.
func ResolvePipeline(pipeline *pipelinespec.Pipeline, variables []model.RuntimeVariable) error {
	if pipeline == nil {
		return nil
	}
	resolved, err := resolveVariableMap(variables)
	if err != nil {
		return err
	}

	return resolvePipelineFields(pipeline, resolved, envVariableResolver)
}

// ResolvePipelineInput substitutes non-secret per-run input values in runtime fields.
// Values are request-local: callers must persist only InputParameterKeys output, never values.
func ResolvePipelineInput(pipeline *pipelinespec.Pipeline, input map[string]any) error {
	if pipeline == nil {
		return nil
	}
	resolved, err := resolveInputMap(input)
	if err != nil {
		return err
	}

	return resolvePipelineFields(pipeline, resolved, inputVariableResolver)
}

// ResolvePipelineSecrets substitutes write-only environment secret values inside the runtime boundary.
func ResolvePipelineSecrets(pipeline *pipelinespec.Pipeline, secrets []model.RuntimeSecretValue) error {
	if pipeline == nil {
		return nil
	}
	resolved := resolveSecretMap(secrets)
	return resolvePipelineFields(pipeline, resolved, secretVariableResolver)
}

func resolvePipelineFields(
	pipeline *pipelinespec.Pipeline,
	resolved map[string]resolvedVariable,
	resolver variableResolver,
) error {
	for i := range pipeline.Steps {
		step := &pipeline.Steps[i]
		if step.Query != nil {
			var err error
			if step.Query.Parameters, err = resolveMap(step.Query.Parameters, resolved, resolver); err != nil {
				return stepError(i, step.Slug.String(), "query parameters", err)
			}
			if step.Query.Parameters == nil {
				step.Query.Parameters = map[string]any{}
			}
			if err = bindOperationReferences(step.Query.OperationInline, resolved, resolver, step.Query.Parameters); err != nil {
				return stepError(i, step.Slug.String(), "query operation_inline", err)
			}
			if step.Query.Incremental != nil && step.Query.Incremental.LookbackWindow != "" {
				lookbackWindow := step.Query.Incremental.LookbackWindow
				value, err := resolveString(lookbackWindow, resolved, resolver)
				if err != nil {
					return stepError(i, step.Slug.String(), "query incremental.lookback_window", err)
				}
				var asString string
				switch typed := value.(type) {
				case string:
					asString = typed
				case LiteralString:
					asString = string(typed)
				default:
					return stepError(
						i,
						step.Slug.String(),
						"query incremental.lookback_window",
						xerror.InvalidInput(resolver.entity).Detail("lookback_window must resolve to a string"),
					)
				}
				if lookbackWindow != asString || isNamespaceReference(lookbackWindow, resolver.namespace) {
					if err := validateLookbackWindow(asString, resolver.entity); err != nil {
						return stepError(i, step.Slug.String(), "query incremental.lookback_window", err)
					}
				}
				step.Query.Incremental.LookbackWindow = asString
			}
		}
		if step.Sink != nil {
			var err error
			if step.Sink.Config == nil {
				step.Sink.Config = map[string]any{}
			}
			if step.Sink.Config, err = resolveMap(step.Sink.Config, resolved, resolver); err != nil {
				return stepError(i, step.Slug.String(), "sink config", err)
			}
			if err = bindOperationReferences(step.Sink.OperationInline, resolved, resolver, step.Sink.Config); err != nil {
				return stepError(i, step.Slug.String(), "sink operation_inline", err)
			}
		}
	}
	return nil
}

func resolveVariableMap(variables []model.RuntimeVariable) (map[string]resolvedVariable, error) {
	resolved := make(map[string]resolvedVariable, len(variables))
	for _, variable := range variables {
		value, err := typedValue(variable)
		if err != nil {
			return nil, err
		}
		if variable.ValueType == model.RuntimeVariableValueTypeText && literalRuntimeValue(variable.Value) {
			value = LiteralString(variable.Value)
		}
		resolved[EnvNamespace+variable.Key] = resolvedVariable{
			value: value,
			date:  variable.ValueType == model.RuntimeVariableValueTypeDate,
		}
	}
	return resolved, nil
}

func resolveInputMap(input map[string]any) (map[string]resolvedVariable, error) {
	if err := ValidateInput(input); err != nil {
		return nil, err
	}

	resolved := make(map[string]resolvedVariable, len(input))
	for key, raw := range input {
		value := runtimeInputValue(raw)
		resolved[InputNamespace+key] = resolvedVariable{
			value: value,
			date:  true,
		}
	}
	return resolved, nil
}

func resolveSecretMap(secrets []model.RuntimeSecretValue) map[string]resolvedVariable {
	resolved := make(map[string]resolvedVariable, len(secrets))
	for _, secret := range secrets {
		value := any(secret.Value)
		if literalRuntimeValue(secret.Value) {
			value = LiteralString(secret.Value)
		}
		resolved[SecretNamespace+secret.Key] = resolvedVariable{
			value: value,
		}
	}
	return resolved
}

func typedValue(variable model.RuntimeVariable) (any, error) {
	switch variable.ValueType {
	case model.RuntimeVariableValueTypeText:
		return variable.Value, nil
	case model.RuntimeVariableValueTypeInteger:
		value, err := strconv.ParseInt(variable.Value, 10, 64)
		if err != nil {
			return nil, xerror.InvalidInput(xerror.RuntimeVariable).
				Detailf("runtime variable %s must contain a valid integer", variable.Key)
		}
		return value, nil
	case model.RuntimeVariableValueTypeBoolean:
		value, err := strconv.ParseBool(variable.Value)
		if err != nil {
			return nil, xerror.InvalidInput(xerror.RuntimeVariable).
				Detailf("runtime variable %s must contain true or false", variable.Key)
		}
		return value, nil
	case model.RuntimeVariableValueTypeDate, model.RuntimeVariableValueTypeUUID:
		return variable.Value, nil
	default:
		return nil, xerror.InvalidInput(xerror.RuntimeVariable).
			Detailf("unsupported runtime variable type %q", variable.ValueType)
	}
}

func resolveMap(
	input map[string]any,
	variables map[string]resolvedVariable,
	resolver variableResolver,
) (map[string]any, error) {
	if input == nil {
		return input, nil
	}
	resolved := make(map[string]any, len(input))
	for key, value := range input {
		next, err := resolveValue(value, variables, resolver)
		if err != nil {
			return nil, err
		}
		resolved[key] = next
	}
	return resolved, nil
}

func resolveValue(
	value any,
	variables map[string]resolvedVariable,
	resolver variableResolver,
) (any, error) {
	switch v := value.(type) {
	case string, LiteralTemplate:
		return resolveString(v, variables, resolver)
	case []any:
		resolved := make([]any, len(v))
		for i, item := range v {
			next, err := resolveValue(item, variables, resolver)
			if err != nil {
				return nil, err
			}
			resolved[i] = next
		}
		return resolved, nil
	case map[string]any:
		return resolveMap(v, variables, resolver)
	default:
		return value, nil
	}
}

func resolveString(input any, variables map[string]resolvedVariable, resolver variableResolver) (any, error) {
	return resolveAuthoredText(input, func(expression string) (any, bool, bool, error) {
		parts := strings.Split(expression, "|")
		name := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(name, resolver.namespace) {
			return nil, false, false, nil
		}
		variable, ok := variables[name]
		if !ok {
			return nil, true, true, resolver.missing(name)
		}
		if len(parts) == 1 {
			return variable.value, true, true, nil
		}
		if !variable.date {
			return nil, true, false, invalidDateExpression(name + ": date operations require a date-typed environment variable or ISO date/datetime input")
		}
		value, err := applyDateOperations(variable.value, parts[1:], true)
		if err != nil {
			return nil, true, false, xerror.Wrapf("runtime parameter %s: %w", name, err)
		}
		return value, true, false, nil
	})
}

func bindOperationReferences(value any, variables map[string]resolvedVariable, resolver variableResolver, parameters map[string]any) error {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			if err := bindOperationReferences(item, variables, resolver, parameters); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := bindOperationReferences(item, variables, resolver, parameters); err != nil {
				return err
			}
		}
	case string, LiteralTemplate:
		for _, part := range fragments(v) {
			if part.literal {
				continue
			}
			for _, match := range runtimeReference.FindAllStringSubmatch(part.text, -1) {
				name, operation, hasOperation := strings.Cut(match[1], "|")
				name = strings.TrimSpace(name)
				if !strings.HasPrefix(name, resolver.namespace) {
					continue
				}
				if hasOperation || operation != "" {
					return invalidDateExpression(name + ": place expressions in query.parameters or sink.config and use named connector parameters")
				}
				variable, ok := variables[name]
				if !ok {
					return resolver.missing(name)
				}
				parameters[name] = variable.value
			}
		}
	}
	return nil
}

func runtimeInputValue(value any) any {
	switch typed := value.(type) {
	case string:
		if literalRuntimeValue(typed) {
			return LiteralString(typed)
		}
		return typed
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, nested := range typed {
			resolved[key] = runtimeInputValue(nested)
		}
		return resolved
	case []any:
		resolved := make([]any, len(typed))
		for i, nested := range typed {
			resolved[i] = runtimeInputValue(nested)
		}
		return resolved
	default:
		return value
	}
}

func runtimeInputSource(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case LiteralString:
		return string(typed)
	}

	data, err := json.Marshal(value)
	if err == nil {
		return string(data)
	}
	return fmt.Sprintf("%v", value)
}

func literalRuntimeValue(value string) bool {
	return strings.HasPrefix(value, stepReferencePrefix) || strings.Contains(value, "{{")
}

// LiteralLocations returns deterministic locations whose values must not be
// interpreted as runtime syntax after a JSON clone or deployment reload.
func LiteralLocations(pipeline *pipelinespec.Pipeline) []LiteralLocation {
	if pipeline == nil {
		return nil
	}
	var paths []LiteralLocation
	for i := range pipeline.Steps {
		step := pipeline.Steps[i]
		if step.Query != nil {
			paths = append(paths, literalMapPaths(step.Query.Parameters, []string{"steps", strconv.Itoa(i), "query", "parameters"})...)
			paths = append(paths, literalMapPaths(step.Query.OperationInline, []string{"steps", strconv.Itoa(i), "query", "operation_inline"})...)
		}
		if step.Sink != nil {
			paths = append(paths, literalMapPaths(step.Sink.Config, []string{"steps", strconv.Itoa(i), "sink", "config"})...)
			paths = append(paths, literalMapPaths(step.Sink.OperationInline, []string{"steps", strconv.Itoa(i), "sink", "operation_inline"})...)
		}
	}
	return paths
}

func literalMapPaths(values map[string]any, prefix []string) []LiteralLocation {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var paths []LiteralLocation
	for _, key := range keys {
		paths = append(paths, literalValuePaths(values[key], append(prefix, key))...)
	}
	return paths
}

func literalValuePaths(value any, path []string) []LiteralLocation {
	switch typed := value.(type) {
	case LiteralTemplate:
		return []LiteralLocation{{Path: slices.Clone(path), Ranges: slices.Clone(typed.Ranges)}}
	case LiteralString:
		return []LiteralLocation{{Path: slices.Clone(path)}}
	case map[string]any:
		return literalMapPaths(typed, path)
	case []any:
		var paths []LiteralLocation
		for i, item := range typed {
			paths = append(paths, literalValuePaths(item, append(path, strconv.Itoa(i)))...)
		}
		return paths
	default:
		return nil
	}
}

// RestoreLiteralLocations restores literal trust markers lost by JSON.
func RestoreLiteralLocations(pipeline *pipelinespec.Pipeline, paths []LiteralLocation) bool {
	if pipeline == nil {
		return len(paths) == 0
	}
	for _, location := range paths {
		path := location.Path
		if len(path) < 5 || path[0] != "steps" {
			return false
		}
		stepIndex, err := strconv.Atoi(path[1])
		if err != nil || stepIndex < 0 || stepIndex >= len(pipeline.Steps) {
			return false
		}
		step := &pipeline.Steps[stepIndex]
		var values map[string]any
		switch {
		case path[2] == "query" && step.Query != nil && path[3] == "parameters":
			values = step.Query.Parameters
		case path[2] == "query" && step.Query != nil && path[3] == "operation_inline":
			values = step.Query.OperationInline
		case path[2] == "sink" && step.Sink != nil && path[3] == "config":
			values = step.Sink.Config
		case path[2] == "sink" && step.Sink != nil && path[3] == "operation_inline":
			values = step.Sink.OperationInline
		default:
			return false
		}
		if !restoreLiteralMapValue(values, path[4:], location.Ranges) {
			return false
		}
	}
	return true
}

func restoreLiteralMapValue(values map[string]any, path []string, ranges [][2]int) bool {
	value, ok := values[path[0]]
	if !ok {
		return false
	}
	restored, ok := restoreLiteralValue(value, path[1:], ranges)
	if ok {
		values[path[0]] = restored
	}
	return ok
}

func restoreLiteralValue(value any, path []string, ranges [][2]int) (any, bool) {
	if len(path) == 0 {
		text, ok := value.(string)
		if !ok || !validLiteralRanges(text, ranges) {
			return nil, false
		}
		if len(ranges) > 0 {
			return LiteralTemplate{Text: text, Ranges: slices.Clone(ranges)}, true
		}
		return LiteralString(text), true
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, restoreLiteralMapValue(typed, path, ranges)
	case []any:
		index, err := strconv.Atoi(path[0])
		if err != nil || index < 0 || index >= len(typed) {
			return nil, false
		}
		restored, ok := restoreLiteralValue(typed[index], path[1:], ranges)
		if ok {
			typed[index] = restored
		}
		return typed, ok
	default:
		return nil, false
	}
}

func missingVariable(name string) error {
	return xerror.InvalidInput(xerror.RuntimeVariable).
		Detailf("runtime variable %s is not defined for the selected environment", strings.TrimPrefix(name, EnvNamespace))
}

func missingInput(name string) error {
	return xerror.InvalidInput(xerror.PipelineRun).
		Detailf("runtime input %s is not defined for this run", strings.TrimPrefix(name, InputNamespace))
}

func missingSecret(name string) error {
	return xerror.InvalidInput(xerror.EnvironmentSecret).
		Detailf("environment secret %s is not defined for the selected environment", strings.TrimPrefix(name, SecretNamespace))
}

func validateLookbackWindow(lookbackWindow string, entity xerror.Entity) error {
	duration, err := time.ParseDuration(lookbackWindow)
	if err != nil || duration <= 0 {
		return xerror.InvalidInput(entity).Detail("lookback_window must resolve to a positive duration")
	}
	return nil
}

func stepError(index int, stepSlug, field string, err error) error {
	return xerror.Wrapf("validate steps: step[%d] %q %s: %w", index, stepSlug, field, err)
}

func IsRuntimeVariableReference(input string) bool {
	return isNamespaceReference(input, EnvNamespace)
}

func IsEnvironmentSecretReference(input string) bool {
	return isNamespaceReference(input, SecretNamespace)
}

func isNamespaceReference(input, namespace string) bool {
	matches := placeholderPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) != 1 || matches[0][0] != 0 || matches[0][1] != len(input) {
		return false
	}
	name := input[matches[0][2]:matches[0][3]]
	return strings.HasPrefix(name, namespace)
}

func HasRuntimeVariableReference(input string) bool {
	return hasNamespaceReference(input, EnvNamespace)
}

func hasNamespaceReference(input, namespace string) bool {
	matches := placeholderPattern.FindAllStringSubmatchIndex(input, -1)
	for _, match := range matches {
		name := input[match[2]:match[3]]
		if strings.HasPrefix(name, namespace) {
			return true
		}
	}
	return false
}

func HasEnvironmentSecretReference(input string) bool {
	return hasNamespaceReference(input, SecretNamespace)
}

func hasNamespaceReferenceInMap(input map[string]any, namespace string) bool {
	for _, value := range input {
		if hasNamespaceReferenceInValue(value, namespace) {
			return true
		}
	}
	return false
}

func hasNamespaceReferenceInValue(value any, namespace string) bool {
	switch typed := value.(type) {
	case LiteralTemplate:
		for _, part := range fragments(typed) {
			if !part.literal && hasNamespaceReference(part.text, namespace) {
				return true
			}
		}
	case string:
		return hasNamespaceReference(typed, namespace)
	case []any:
		for _, item := range typed {
			if hasNamespaceReferenceInValue(item, namespace) {
				return true
			}
		}
	case map[string]any:
		return hasNamespaceReferenceInMap(typed, namespace)
	}
	return false
}

// HasEnvironmentSecretReferenceInPipeline reports whether runtime secret resolution is needed.
func HasEnvironmentSecretReferenceInPipeline(pipeline *pipelinespec.Pipeline) bool {
	if pipeline == nil {
		return false
	}
	for i := range pipeline.Steps {
		step := &pipeline.Steps[i]
		if step.Query != nil {
			if hasNamespaceReferenceInMap(step.Query.Parameters, SecretNamespace) ||
				hasNamespaceReferenceInMap(step.Query.OperationInline, SecretNamespace) ||
				(step.Query.Incremental != nil &&
					hasNamespaceReference(step.Query.Incremental.LookbackWindow, SecretNamespace)) {
				return true
			}
		}
		if step.Sink != nil &&
			(hasNamespaceReferenceInMap(step.Sink.OperationInline, SecretNamespace) ||
				hasNamespaceReferenceInMap(step.Sink.Config, SecretNamespace)) {
			return true
		}
	}
	return false
}

func VariableReference(key string) string {
	return fmt.Sprintf("{{%s%s}}", EnvNamespace, key)
}

func SecretReference(key string) string {
	return fmt.Sprintf("{{%s%s}}", SecretNamespace, key)
}

// ValidateInput enforces the public non-secret runtime input key contract.
func ValidateInput(input map[string]any) error {
	for key := range input {
		if err := ValidateInputKey(key); err != nil {
			return err
		}
	}
	return nil
}

// ValidateInputKey checks whether a per-run input key can be referenced as runtime.input.key.
func ValidateInputKey(key string) error {
	if !runtimeInputKeyPattern.MatchString(key) {
		return xerror.InvalidInput(xerror.PipelineRun).
			Detail("runtime input key must match [A-Za-z_][A-Za-z0-9_]*")
	}
	tokens := runtimeInputKeyTokens(key)
	for _, fragment := range secretRuntimeInputKeyFragments {
		if runtimeInputKeyContainsSecretToken(tokens, fragment) {
			return xerror.InvalidInput(xerror.PipelineRun).
				Detail("runtime input values are non-secret; store credentials on concrete data sources or secret storage")
		}
	}
	return nil
}

// runtimeInputKeyTokens splits safe identifier keys into lowercase underscore and camelCase tokens.
func runtimeInputKeyTokens(key string) []string {
	tokens := make([]string, 0, 2)
	current := make([]rune, 0, len(key))
	keyRunes := []rune(key)
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, string(current))
		current = current[:0]
	}

	for index, value := range keyRunes {
		if value == '_' {
			flush()
			continue
		}
		if unicode.IsUpper(value) && len(current) > 0 {
			previous := keyRunes[index-1]
			hasLowercaseNext := index+1 < len(keyRunes) && unicode.IsLower(keyRunes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && hasLowercaseNext) {
				flush()
			}
		}
		current = append(current, unicode.ToLower(value))
	}
	flush()
	return tokens
}

// runtimeInputKeyContainsSecretToken checks secret fragments on underscore token boundaries.
func runtimeInputKeyContainsSecretToken(tokens []string, fragment string) bool {
	fragmentTokens := strings.Split(fragment, "_")
	if len(fragmentTokens) == 0 || len(fragmentTokens) > len(tokens) {
		return false
	}
	for i := 0; i <= len(tokens)-len(fragmentTokens); i++ {
		matches := true
		for j, fragmentToken := range fragmentTokens {
			if tokens[i+j] != fragmentToken {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// InputParameterKeys returns the sorted safe parameter-key names for run source metadata.
func InputParameterKeys(input map[string]any) []string {
	if len(input) == 0 {
		return nil
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, "input."+key)
	}
	sort.Strings(keys)
	return keys
}

// EnvironmentKeys lists only references in fields ResolvePipeline substitutes.
func EnvironmentKeys(pipeline *pipelinespec.Pipeline) []string {
	keys := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case string:
			for _, match := range runtimeReference.FindAllStringSubmatch(value, -1) {
				name, _, _ := strings.Cut(match[1], "|")
				if key, ok := strings.CutPrefix(strings.TrimSpace(name), EnvNamespace); ok {
					keys[key] = true
				}
			}
		case map[string]any:
			for _, nested := range value {
				visit(nested)
			}
		case []any:
			for _, nested := range value {
				visit(nested)
			}
		}
	}
	if pipeline != nil {
		for _, step := range pipeline.Steps {
			if step.Query != nil {
				visit(step.Query.Parameters)
				visit(step.Query.OperationInline)
				if step.Query.Incremental != nil {
					visit(step.Query.Incremental.LookbackWindow)
				}
			}
			if step.Sink != nil {
				visit(step.Sink.OperationInline)
				visit(step.Sink.Config)
			}
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
