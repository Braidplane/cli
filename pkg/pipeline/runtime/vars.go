package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	RunDateParamName     = Namespace + "run_date"
	RunDateTimeParamName = Namespace + "run_datetime"
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
	source string
	value  any
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
			if step.Query.OperationInline, err = resolveMap(step.Query.OperationInline, resolved, resolver); err != nil {
				return stepError(i, step.Slug.String(), "query operation_inline", err)
			}
			step.Query.OperationInline = unwrapLiteralStringsInMap(step.Query.OperationInline)
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
			if step.Sink.OperationInline, err = resolveMap(step.Sink.OperationInline, resolved, resolver); err != nil {
				return stepError(i, step.Slug.String(), "sink operation_inline", err)
			}
			step.Sink.OperationInline = unwrapLiteralStringsInMap(step.Sink.OperationInline)
			if step.Sink.Config, err = resolveMap(step.Sink.Config, resolved, resolver); err != nil {
				return stepError(i, step.Slug.String(), "sink config", err)
			}
			step.Sink.Config = unwrapLiteralStringsInMap(step.Sink.Config)
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
		if variable.ValueType == model.RuntimeVariableValueTypeText &&
			strings.HasPrefix(variable.Value, stepReferencePrefix) {
			value = LiteralString(variable.Value)
		}
		resolved[EnvNamespace+variable.Key] = resolvedVariable{
			source: variable.Value,
			value:  value,
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
			source: runtimeInputSource(raw),
			value:  value,
		}
	}
	return resolved, nil
}

func resolveSecretMap(secrets []model.RuntimeSecretValue) map[string]resolvedVariable {
	resolved := make(map[string]resolvedVariable, len(secrets))
	for _, secret := range secrets {
		value := any(secret.Value)
		if strings.HasPrefix(secret.Value, stepReferencePrefix) {
			value = LiteralString(secret.Value)
		}
		resolved[SecretNamespace+secret.Key] = resolvedVariable{
			source: secret.Value,
			value:  value,
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
	case string:
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

func resolveString(
	input string,
	variables map[string]resolvedVariable,
	resolver variableResolver,
) (any, error) {
	matches := placeholderPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, nil
	}

	exact := len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(input)
	if exact {
		name := input[matches[0][2]:matches[0][3]]
		if !strings.HasPrefix(name, resolver.namespace) {
			return input, nil
		}
		variable, ok := variables[name]
		if !ok {
			return nil, resolver.missing(name)
		}
		return variable.value, nil
	}

	var builder strings.Builder
	last := 0
	leadingStepLikeLiteral := false
	for _, match := range matches {
		name := input[match[2]:match[3]]
		if !strings.HasPrefix(name, resolver.namespace) {
			continue
		}
		variable, ok := variables[name]
		if !ok {
			return nil, resolver.missing(name)
		}
		builder.WriteString(input[last:match[0]])
		if last == 0 && match[0] == 0 {
			_, leadingStepLikeLiteral = variable.value.(LiteralString)
		}
		builder.WriteString(variable.source)
		last = match[1]
	}
	if last == 0 {
		return input, nil
	}
	builder.WriteString(input[last:])
	resolved := builder.String()
	if leadingStepLikeLiteral && strings.HasPrefix(resolved, stepReferencePrefix) {
		return LiteralString(resolved), nil
	}
	return resolved, nil
}

func runtimeInputValue(value any) any {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, stepReferencePrefix) {
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

func unwrapLiteralStringsInMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	unwrapped := make(map[string]any, len(input))
	for key, value := range input {
		unwrapped[key] = unwrapLiteralStringValue(value)
	}
	return unwrapped
}

func unwrapLiteralStringValue(value any) any {
	switch typed := value.(type) {
	case LiteralString:
		return string(typed)
	case map[string]any:
		return unwrapLiteralStringsInMap(typed)
	case []any:
		unwrapped := make([]any, len(typed))
		for i, item := range typed {
			unwrapped[i] = unwrapLiteralStringValue(item)
		}
		return unwrapped
	default:
		return value
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
			for _, match := range placeholderPattern.FindAllStringSubmatch(value, -1) {
				if key, ok := strings.CutPrefix(match[1], EnvNamespace); ok {
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
