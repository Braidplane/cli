package sql

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/parser/opcode"
	"github.com/pingcap/tidb/pkg/parser/test_driver"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

var (
	jsonIndexShorthandPattern          = regexp.MustCompile(`->\s*\[\s*(\d+)\s*\]`)
	transformInputRelationNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	normalizedTransformSQLTokenPattern = regexp.MustCompile("`(?:``|[^`])*`|[^\\s`]+")
)

// ValidateTransformSQL enforces the sql_v1 MVP contract for transform DSL.
// Returns (warnings, error): warnings are informational hints to surface to the user;
// they do not block the statement from being accepted.
//
// Validation checks the supported SQL syntax and structure but does not
// infer or validate field types. Type enforcement is handled separately in
// ValidateOutputContractTypes once datasource context is available.
func ValidateTransformSQL(sql string) ([]string, error) {
	_, hints, err := parseTransformSelect(sql)
	return hints, err
}

func ValidateTransformInputRelationName(name string) error {
	if !validTransformRelationIdentifier(name) {
		return xerror.InvalidInput(xerror.Pipeline).Detailf(
			"transform input name %q must be a valid unquoted SQL relation identifier",
			name,
		)
	}

	return nil
}

// validTransformRelationIdentifier checks the lexical and parser-level shape
// shared by transform input names and CTE relation names.
func validTransformRelationIdentifier(name string) bool {
	if !transformInputRelationNamePattern.MatchString(name) {
		return false
	}

	p := parser.New()
	_, _, err := p.ParseSQL("SELECT * FROM " + name)
	return err == nil
}

// ValidateOutputContractStructure validates only projection/contract structure.
// It is intended for load-time checks where datasource schema context is unavailable.
func ValidateOutputContractStructure(sql string, contract *pipepayload.Contract) error {
	if contract == nil {
		return nil
	}

	_, err := parseAndValidateOutputContractStructure(sql, contract)
	return err
}

// ValidateOutputContractTypesWithInputSchema validates output contract types
// using datasource context plus inferred input field types from schema introspection.
//
// inputFieldTypes is a map of source field name -> contract field type for input columns/fields.
func ValidateOutputContractTypesWithInputSchema(
	sql string,
	contract *pipepayload.Contract,
	dataSourceType model.DataSourceType,
	inputFieldTypes map[string]model.ContractFieldType,
) error {
	return ValidateOutputContractTypesWithInputSchemaForOutputKind(
		sql,
		model.TransformOutputKindUnknown,
		contract,
		dataSourceType,
		inputFieldTypes,
	)
}

// ValidateOutputContractTypesWithInputSchemaForOutputKind validates output
// contract types after applying output_kind-specific serialization semantics.
func ValidateOutputContractTypesWithInputSchemaForOutputKind(
	sql string,
	outputKind model.TransformOutputKind,
	contract *pipepayload.Contract,
	dataSourceType model.DataSourceType,
	inputFieldTypes map[string]model.ContractFieldType,
) error {
	if contract == nil {
		return nil
	}
	if dataSourceType == model.DataSourceTypeUnknown && len(inputFieldTypes) == 0 {
		return ErrTypeInput.Detail("unknown datasource type")
	}

	selectStmt, err := parseAndValidateOutputContractStructure(sql, contract)
	if err != nil {
		return err
	}

	transformInputTypes := transformInputFieldTypes(inputFieldTypes)
	expandedInputTypes, inputAliases, relationScope, err := prepareTransformTypeContext(
		selectStmt,
		dataSourceType,
		transformInputTypes,
	)
	if err != nil {
		return err
	}

	if err := validateTransformInputReferencesWithRelationScope(
		selectStmt,
		expandedInputTypes,
		inputAliases,
		relationScope,
	); err != nil {
		return err
	}

	projectionWithTypes, err := extractTransformProjectionWithTypeDetails(
		selectStmt,
		dataSourceType,
		expandedInputTypes,
		inputAliases,
	)
	if err != nil {
		return err
	}
	mergeProjectionTypeDetails(
		projectionWithTypes,
		extractTransformNestedJSONProjectionWithTypeDetails(
			selectStmt,
			dataSourceType,
			expandedInputTypes,
			inputAliases,
		),
	)

	for _, field := range contract.Fields {
		projected, exists := projectionWithTypes[field.Name]
		if !exists {
			if constructedRoot, ok := constructedJSONRootProjection(field.Name, projectionWithTypes); ok {
				if !field.Required {
					continue
				}
				if outputKind != model.TransformOutputKindJSON {
					return ErrContractType.Detailf(
						"output contract field %q cannot be validated because constructed JSON projection %q is serialized as a string for row-oriented output",
						field.Name,
						constructedRoot,
					)
				}
				return ErrContractType.Detailf(
					"output contract field %q has uncertain projection type",
					field.Name,
				)
			}
			continue
		}

		if constructedRoot, ok := constructedJSONRootProjection(field.Name, projectionWithTypes); ok &&
			outputKind != model.TransformOutputKindJSON {
			if !field.Required {
				continue
			}
			return ErrContractType.Detailf(
				"output contract field %q cannot be validated because constructed JSON projection %q is serialized as a string for row-oriented output",
				field.Name,
				constructedRoot,
			)
		}

		projectedType := projected.typeForOutputKind(outputKind)
		if projectedType == model.ContractFieldTypeUnknown {
			return ErrContractType.Detailf(
				"output contract field %q has uncertain projection type",
				field.Name,
			)
		}
		if projectedType == model.ContractFieldTypeValueDecimal && field.Type == model.ContractFieldTypeValueDecimal && projected.precision != nil {
			producer := pipepayload.ContractField{Type: projectedType, Precision: projected.precision, Scale: projected.scale}
			if !pipepayload.DecimalFieldCompatible(producer, field) {
				return ErrContractType.Detailf("decimal output field %q has incompatible precision or scale", field.Name)
			}
		}
		if !isCompatibleContractFieldType(projectedType, field.Type) {
			return ErrContractType.Detailf(
				"output contract field %q expects %s but projection resolves to %s",
				field.Name,
				field.Type,
				projectedType,
			)
		}
	}

	return nil
}

// InferOutputContractWithInputSchema derives a transform output contract from
// the SELECT projection using the same schema-aware inference as validation.
func InferOutputContractWithInputSchema(
	sql string,
	outputKind model.TransformOutputKind,
	dataSourceType model.DataSourceType,
	inputFieldTypes map[string]model.ContractFieldType,
) (*pipepayload.Contract, error) {
	if dataSourceType == model.DataSourceTypeUnknown && len(inputFieldTypes) == 0 {
		return nil, ErrTypeInput.Detail("unknown datasource type")
	}

	selectStmt, _, err := parseTransformSelect(sql)
	if err != nil {
		return nil, err
	}

	transformInputTypes := transformInputFieldTypes(inputFieldTypes)
	expandedInputTypes, inputAliases, relationScope, err := prepareTransformTypeContext(
		selectStmt,
		dataSourceType,
		transformInputTypes,
	)
	if err != nil {
		return nil, err
	}
	if err := validateTransformInputReferencesWithRelationScope(
		selectStmt,
		expandedInputTypes,
		inputAliases,
		relationScope,
	); err != nil {
		return nil, err
	}

	projectionWithTypes, err := extractTransformProjectionWithTypeDetails(
		selectStmt,
		dataSourceType,
		expandedInputTypes,
		inputAliases,
	)
	if err != nil {
		return nil, err
	}

	fieldNames := make([]string, 0, len(projectionWithTypes))
	for fieldName := range projectionWithTypes {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	fields := make([]pipepayload.ContractField, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		fieldType := projectionWithTypes[fieldName].typeForOutputKind(outputKind)
		if fieldType == model.ContractFieldTypeUnknown {
			return nil, ErrContractType.Detailf("output contract field %q has uncertain projection type", fieldName)
		}
		detail := projectionWithTypes[fieldName]
		if fieldType == model.ContractFieldTypeValueDecimal && detail.precision == nil {
			return nil, ErrContractType.Detailf("decimal output field %q requires an explicit output contract or final DECIMAL(p,s) cast", fieldName)
		}
		fields = append(fields, pipepayload.ContractField{
			Name:      fieldName,
			Type:      fieldType,
			Nullable:  true,
			Precision: detail.precision,
			Scale:     detail.scale,
		})
	}

	representation, rootShape := outputKindContractFacets(outputKind)
	return &pipepayload.Contract{
		Representation: representation,
		RootShape:      rootShape,
		Fields:         fields,
	}, nil
}

// outputKindContractFacets maps transform output_kind to static contract facets
// used by inferred contracts before any runtime payload exists.
func outputKindContractFacets(
	outputKind model.TransformOutputKind,
) (model.ContractRepresentation, model.ContractRootShape) {
	switch outputKind {
	case model.TransformOutputKindCSV:
		return model.ContractRepresentationCSV, model.ContractRootShapeUnknown
	case model.TransformOutputKindJSON:
		return model.ContractRepresentationJSON, model.ContractRootShapeArray
	default:
		return model.ContractRepresentationRows, model.ContractRootShapeUnknown
	}
}

// parseAndValidateOutputContractStructure parses SQL once and applies
// structure validation (projection aliases/required fields). It is shared by both
// load-time and type-aware validation paths to avoid duplicate parsing logic.
func parseAndValidateOutputContractStructure(
	sql string,
	contract *pipepayload.Contract,
) (*ast.SelectStmt, error) {
	selectStmt, _, err := parseTransformSelect(sql)
	if err != nil {
		return nil, err
	}
	if err := validateOutputContractPresence(selectStmt, contract); err != nil {
		return nil, err
	}

	return selectStmt, nil
}

func validateOutputContractPresence(
	selectStmt *ast.SelectStmt,
	contract *pipepayload.Contract,
) error {
	projectionFields, err := extractProjectionFieldNames(selectStmt)
	if err != nil {
		return err
	}

	if err := pipepayload.ValidateContractFieldDeclarations(contract); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(contract.Fields))
	for _, field := range contract.Fields {
		if _, exists := seen[field.Name]; exists {
			return ErrContractInvalid.Detailf(
				"duplicate output contract field %q",
				field.Name,
			)
		}
		seen[field.Name] = struct{}{}

		// For nested paths (e.g. "profile.email", "items[].sku"), only the root
		// segment must appear in the SQL projection; nested resolution happens at runtime.
		rootSegment := strings.SplitN(field.Name, ".", 2)[0]
		rootSegment = strings.TrimSuffix(rootSegment, "[]")
		if _, exists := projectionFields[rootSegment]; !exists && field.Required {
			return ErrContractMissing.Detailf(
				"required output contract field %q is missing from SELECT projection",
				field.Name,
			)
		}
	}

	return nil
}

type transformProjectionTypeDetail struct {
	fieldType       model.ContractFieldType
	constructedJSON bool
	precision       *int
	scale           *int
}

// typeForOutputKind returns the contract-visible field type after transform
// output serialization has been applied.
func (d transformProjectionTypeDetail) typeForOutputKind(
	outputKind model.TransformOutputKind,
) model.ContractFieldType {
	if outputKind == model.TransformOutputKindCSV && d.fieldType != model.ContractFieldTypeValueDecimal {
		return model.ContractFieldTypeValueString
	}
	if d.constructedJSON &&
		(outputKind == model.TransformOutputKindRows ||
			outputKind == model.TransformOutputKindUnknown) {
		return model.ContractFieldTypeValueString
	}
	return d.fieldType
}

// extractTransformProjectionWithTypes extracts field names and inferred types from
// the SELECT projection during post-load validation.
// Types are inferred using the provided dataSourceType with datasource-specific rules.
// Unknown means the type cannot be proven in this phase and is treated as contract failure by
// ValidateOutputContractTypes.
// Returns a map of field_name -> ContractFieldType. Unknown types map to ContractFieldTypeUnknown.
func extractTransformProjectionWithTypes(
	selectStmt *ast.SelectStmt,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) (map[string]model.ContractFieldType, error) {
	details, err := extractTransformProjectionWithTypeDetails(
		selectStmt,
		dataSourceType,
		inputFieldTypes,
		inputAliases,
	)
	if err != nil {
		return nil, err
	}

	result := make(map[string]model.ContractFieldType)
	for fieldName, detail := range details {
		result[fieldName] = detail.fieldType
	}

	return result, nil
}

// extractTransformProjectionWithTypeDetails extracts field names, inferred
// types, and SQL-helper provenance from the SELECT projection.
func extractTransformProjectionWithTypeDetails(
	selectStmt *ast.SelectStmt,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) (map[string]transformProjectionTypeDetail, error) {
	result := make(map[string]transformProjectionTypeDetail)

	if selectStmt.Fields == nil || len(selectStmt.Fields.Fields) == 0 {
		return result, nil
	}

	for _, field := range selectStmt.Fields.Fields {
		fieldName, err := projectionFieldName(field)
		if err != nil {
			return nil, xerror.Wrapf("resolve projection field name for type inference: %w", err)
		}
		fieldType := inferTransformFieldTypeForDataSource(
			field.Expr,
			dataSourceType,
			inputFieldTypes,
			inputAliases,
		)
		var precision, scale *int
		if fieldType == model.ContractFieldTypeValueDecimal {
			precision, scale = decimalProjectionDeclaration(field.Expr)
		}
		result[fieldName] = transformProjectionTypeDetail{
			precision: precision, scale: scale,
			fieldType:       fieldType,
			constructedJSON: isJSONConstructionProjectionExpr(field.Expr),
		}
	}

	return result, nil
}

// constructedJSONRootProjection returns the projected root alias when a nested
// contract path points inside a constructed JSON value.
func constructedJSONRootProjection(
	fieldName string,
	projectionWithTypes map[string]transformProjectionTypeDetail,
) (string, bool) {
	if !isNestedContractFieldPath(fieldName) {
		return "", false
	}

	root := contractFieldRootSegment(fieldName)
	if root == "" {
		return "", false
	}

	projected, ok := projectionWithTypes[root]
	return root, ok && projected.constructedJSON
}

// isNestedContractFieldPath reports whether a contract field uses a path below
// a projected root, including array element traversal.
func isNestedContractFieldPath(fieldName string) bool {
	return strings.Contains(fieldName, ".") || strings.Contains(fieldName, "[]")
}

// contractFieldRootSegment extracts the projection alias that owns a contract
// field path such as "profile.email" or "items[].sku".
func contractFieldRootSegment(fieldName string) string {
	root, _, _ := strings.Cut(fieldName, ".")
	return strings.TrimSuffix(root, "[]")
}

// extractTransformNestedJSONProjectionWithTypeDetails derives nested contract
// paths for constructed JSON projections without changing row/CTE inference.
func extractTransformNestedJSONProjectionWithTypeDetails(
	selectStmt *ast.SelectStmt,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) map[string]transformProjectionTypeDetail {
	result := make(map[string]transformProjectionTypeDetail)
	if selectStmt == nil || selectStmt.Fields == nil {
		return result
	}

	for _, field := range selectStmt.Fields.Fields {
		if field == nil || !isJSONConstructionProjectionExpr(field.Expr) {
			continue
		}

		fieldName, err := projectionFieldName(field)
		if err != nil {
			continue
		}
		mergeProjectionTypeDetails(
			result,
			extractJSONConstructionNestedTypeDetails(
				fieldName,
				field.Expr,
				dataSourceType,
				inputFieldTypes,
				inputAliases,
			),
		)
	}
	return result
}

// extractJSONConstructionNestedTypeDetails derives contract-visible nested
// field types from supported JSON construction helper expressions.
func extractJSONConstructionNestedTypeDetails(
	rootFieldName string,
	expr ast.ExprNode,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) map[string]transformProjectionTypeDetail {
	result := make(map[string]transformProjectionTypeDetail)
	collectJSONConstructionNestedTypeDetails(
		result,
		rootFieldName,
		expr,
		dataSourceType,
		inputFieldTypes,
		inputAliases,
	)
	return result
}

// collectJSONConstructionNestedTypeDetails walks the JSON helper AST and adds
// only paths whose keys and element traversal are statically knowable.
func collectJSONConstructionNestedTypeDetails(
	result map[string]transformProjectionTypeDetail,
	prefix string,
	expr ast.ExprNode,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) {
	fn, ok := expr.(*ast.FuncCallExpr)
	if !ok || fn == nil || fn.FnName.L == "" {
		return
	}

	switch strings.ToLower(fn.FnName.L) {
	case jsonFuncObject:
		if len(fn.Args)%2 != 0 {
			return
		}
		for i := 0; i < len(fn.Args); i += 2 {
			key, ok := jsonObjectKeyLiteral(fn.Args[i])
			if !ok || key == "" {
				continue
			}

			childPath := jsonObjectChildPath(prefix, key)
			childExpr := fn.Args[i+1]
			mergeProjectionTypeDetail(result, childPath, transformProjectionTypeDetail{
				fieldType: inferTransformFieldTypeForDataSource(
					childExpr,
					dataSourceType,
					inputFieldTypes,
					inputAliases,
				),
				constructedJSON: isJSONConstructionProjectionExpr(childExpr),
			})
			collectJSONConstructionNestedTypeDetails(
				result,
				childPath,
				childExpr,
				dataSourceType,
				inputFieldTypes,
				inputAliases,
			)
		}
	case jsonFuncArray:
		elementPath := prefix + "[]"
		var commonElementDetails map[string]transformProjectionTypeDetail
		for i, arg := range fn.Args {
			elementDetails := make(map[string]transformProjectionTypeDetail)
			mergeProjectionTypeDetail(elementDetails, elementPath, transformProjectionTypeDetail{
				fieldType: inferTransformFieldTypeForDataSource(
					arg,
					dataSourceType,
					inputFieldTypes,
					inputAliases,
				),
				constructedJSON: isJSONConstructionProjectionExpr(arg),
			})
			collectJSONConstructionNestedTypeDetails(
				elementDetails,
				elementPath,
				arg,
				dataSourceType,
				inputFieldTypes,
				inputAliases,
			)

			if i == 0 {
				commonElementDetails = elementDetails
				continue
			}
			commonElementDetails = intersectProjectionTypeDetails(
				commonElementDetails,
				elementDetails,
			)
		}
		mergeProjectionTypeDetails(result, commonElementDetails)
	}
}

// jsonObjectKeyLiteral returns statically knowable JSON_OBJECT keys. Runtime
// supports only values that evaluate to strings, but static path validation is
// intentionally limited to literal keys.
func jsonObjectKeyLiteral(expr ast.ExprNode) (string, bool) {
	valueExpr, ok := expr.(ast.ValueExpr)
	if !ok {
		return "", false
	}
	value, ok := valueExpr.GetValue().(string)
	return value, ok
}

// jsonObjectChildPath appends an object key to a contract field path.
func jsonObjectChildPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// mergeProjectionTypeDetails adds derived nested projection details while
// preserving conservative Unknown results when duplicate paths disagree.
func mergeProjectionTypeDetails(
	target map[string]transformProjectionTypeDetail,
	source map[string]transformProjectionTypeDetail,
) {
	for fieldName, detail := range source {
		mergeProjectionTypeDetail(target, fieldName, detail)
	}
}

// intersectProjectionTypeDetails keeps only paths that every array element
// argument proves, preserving Unknown on type conflicts.
func intersectProjectionTypeDetails(
	left map[string]transformProjectionTypeDetail,
	right map[string]transformProjectionTypeDetail,
) map[string]transformProjectionTypeDetail {
	result := make(map[string]transformProjectionTypeDetail)
	for fieldName, leftDetail := range left {
		rightDetail, exists := right[fieldName]
		if !exists {
			continue
		}
		mergeProjectionTypeDetail(result, fieldName, leftDetail)
		mergeProjectionTypeDetail(result, fieldName, rightDetail)
	}
	return result
}

// mergeProjectionTypeDetail stores one inferred path type and marks conflicts
// as Unknown so contract validation fails closed.
func mergeProjectionTypeDetail(
	target map[string]transformProjectionTypeDetail,
	fieldName string,
	detail transformProjectionTypeDetail,
) {
	existing, exists := target[fieldName]
	if !exists {
		target[fieldName] = detail
		return
	}

	if existing.fieldType != detail.fieldType {
		existing.fieldType = model.ContractFieldTypeUnknown
	}
	existing.constructedJSON = existing.constructedJSON || detail.constructedJSON
	target[fieldName] = existing
}

// isJSONConstructionProjectionExpr reports whether a projection's visible value
// is produced by a JSON construction helper that row-oriented outputs stringify.
func isJSONConstructionProjectionExpr(expr ast.ExprNode) bool {
	fn, ok := expr.(*ast.FuncCallExpr)
	if !ok || fn == nil || fn.FnName.L == "" {
		return false
	}
	switch strings.ToLower(fn.FnName.L) {
	case jsonFuncObject, jsonFuncArray:
		return true
	default:
		return false
	}
}

// parseTransformSelect parses SQL text as a SELECT statement and validates it against
// the sql_v1 MVP surface. Returns the parsed AST, parser warnings (as hints), and any
// errors.
func parseTransformSelect(sql string) (*ast.SelectStmt, []string, error) {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return nil, nil, ErrRequired
	}

	normalized := normalizeTransformSQLShorthand(trimmed)

	p := parser.New()
	statements, warns, err := p.ParseSQL(normalized)
	if err != nil {
		return nil, nil, xerror.Wrapf("parse transform sql: %w", err)
	}

	// Collect parser warnings as user-facing hints; they do not fail validation.
	var hints []string
	for _, w := range warns {
		hints = append(hints, w.Error())
	}

	if len(statements) != 1 {
		return nil, hints, ErrMultiStatement
	}

	selectStmt, ok := statements[0].(*ast.SelectStmt)
	if !ok {
		return nil, hints, ErrNotSelect
	}

	// TiDB parses FULL JOIN as an implicit alias followed by an inner join.
	// Explicit AS full and quoted aliases remain ordinary supported aliases.
	// Normalize unescapes doubled backticks; mask those escapes for this check
	// so quoted identifiers remain single tokens without changing the parsed AST.
	joinCheckSQL := strings.ReplaceAll(normalized, "``", "_")
	tokens := normalizedTransformSQLTokenPattern.FindAllString(parser.Normalize(joinCheckSQL, "ON"), -1)
	for i := 1; i+1 < len(tokens); i++ {
		if tokens[i] != "full" || tokens[i+1] != "join" {
			continue
		}
		switch tokens[i-1] {
		case "as", "from", "join", "straight_join", ",", "(", ".":
			// Here full is an explicit alias or the relation name itself.
			continue
		default:
			return nil, hints, ErrFeatureNotAllowed.Detail("FULL JOIN is not allowed")
		}
	}

	if err := validateSelectSubset(selectStmt); err != nil {
		return nil, hints, err
	}

	return selectStmt, hints, nil
}

func normalizeTransformSQLShorthand(sql string) string {
	// Accept source ->[0] shorthand by translating it to TiDB/MySQL-compatible
	// JSON path form source -> '$[0]' before parsing.
	return jsonIndexShorthandPattern.ReplaceAllString(sql, `-> '$[$1]'`)
}

// extractProjectionFieldNames extracts the names of all projected fields from a SELECT statement.
// Validates that projections have explicit aliases (either AS name or column reference).
func extractProjectionFieldNames(selectStmt *ast.SelectStmt) (map[string]struct{}, error) {
	if selectStmt.Fields == nil {
		return nil, ErrContractInvalid.Detail("select projection is required")
	}

	projection := make(map[string]struct{}, len(selectStmt.Fields.Fields))
	for _, field := range selectStmt.Fields.Fields {
		if field.Auxiliary {
			continue
		}
		if field.WildCard != nil {
			return nil, ErrContractInvalid.Detail(
				"wildcard projections are not supported with output contracts",
			)
		}

		name, err := projectionFieldName(field)
		if err != nil {
			return nil, err
		}
		if _, exists := projection[name]; exists {
			return nil, ErrContractInvalid.Detailf("duplicate projected field %q", name)
		}

		projection[name] = struct{}{}
	}

	return projection, nil
}

// projectionFieldName extracts the effective alias for a SELECT field: explicit alias (AS name),
// column name from simple column references, or returns an error for derived expressions without
// aliases.
func projectionFieldName(field *ast.SelectField) (string, error) {
	if field.AsName.L != "" {
		return field.AsName.L, nil
	}

	columnExpr, ok := field.Expr.(*ast.ColumnNameExpr)
	if ok && columnExpr.Name != nil {
		return columnExpr.Name.Name.L, nil
	}

	return "", ErrContractInvalid.Detail(
		"derived select expressions must use an alias to satisfy output contracts",
	)
}

// validateSelectSubset rejects SQL features outside the MVP sql_v1 surface.
func validateSelectSubset(selectStmt *ast.SelectStmt) error {
	if err := validateWithCTESubset(selectStmt.With); err != nil {
		return err
	}

	return validateSelectBodySubset(selectStmt, true)
}

// validateSelectBodySubset applies the normal SELECT subset rules to one SELECT
// body, optionally detaching a top-level WITH before the generic AST visitor runs.
func validateSelectBodySubset(selectStmt *ast.SelectStmt, allowTopLevelWith bool) error {
	if selectStmt == nil {
		return ErrFeatureNotAllowed.Detail("select statement is required")
	}
	if selectStmt.With != nil && !allowTopLevelWith {
		return ErrFeatureNotAllowed.Detail("nested with clauses are not allowed")
	}
	if selectStmt.Distinct {
		return ErrFeatureNotAllowed.Detail("SELECT DISTINCT is not supported")
	}

	if selectStmt.Having != nil {
		return ErrFeatureNotAllowed.Detail("having is not allowed")
	}

	if len(selectStmt.WindowSpecs) > 0 {
		return ErrFeatureNotAllowed.Detail("window specifications are not allowed")
	}

	if err := validateFromInputRows(selectStmt.From); err != nil {
		return err
	}

	// CTE bodies are represented as subquery expressions by the TiDB parser. The
	// CTE validator handles those bodies explicitly, so the generic visitor sees
	// only the SELECT body currently being validated.
	var with *ast.WithClause
	if allowTopLevelWith {
		with = selectStmt.With
		selectStmt.With = nil
		defer func() {
			selectStmt.With = with
		}()
	}

	visitor := &disallowedNodeVisitor{}
	_, ok := selectStmt.Accept(visitor)
	if !ok {
		if visitor.err != nil {
			return visitor.err
		}
		return ErrFeatureNotAllowed.Detail("transform sql validation failed")
	}
	if visitor.err != nil {
		return visitor.err
	}
	if selectStmt.Fields != nil {
		projectionVisitor := &disallowedNodeVisitor{projection: true}
		selectStmt.Fields.Accept(projectionVisitor)
		if projectionVisitor.err != nil {
			return projectionVisitor.err
		}
	}
	if selectStmt.OrderBy != nil {
		projectionVisitor := &disallowedNodeVisitor{projection: true}
		selectStmt.OrderBy.Accept(projectionVisitor)
		if projectionVisitor.err != nil {
			return projectionVisitor.err
		}
		projectionFields, err := extractProjectionFieldNames(selectStmt)
		if err != nil {
			return err
		}
		projectedTypes := make(transformInputFieldTypes, len(projectionFields))
		for name := range projectionFields {
			projectedTypes[name] = model.ContractFieldTypeUnknown
		}
		for _, item := range selectStmt.OrderBy.Items {
			if item == nil {
				continue
			}
			if err := validateTransformInputExpressionReferences(item.Expr, projectedTypes, nil); err != nil {
				return xerror.Wrapf("ORDER BY can reference only unqualified projected fields: %w", err)
			}
		}
	}

	if err := validateGroupBySubset(selectStmt); err != nil {
		return err
	}

	return nil
}

// validateWithCTESubset validates the only supported WITH form: non-recursive
// CTEs declared before use, each with a single SELECT body in the normal subset.
func validateWithCTESubset(with *ast.WithClause) error {
	if with == nil {
		return nil
	}
	if with.IsRecursive {
		return ErrFeatureNotAllowed.Detail("recursive CTEs are not allowed")
	}

	allCTENames := make(map[string]struct{}, len(with.CTEs))
	for _, cte := range with.CTEs {
		cteName := cteRelationName(cte)
		if cteName == "" {
			return ErrFeatureNotAllowed.Detail("cte name is required")
		}
		if !validTransformRelationIdentifier(cteName) {
			return ErrFeatureNotAllowed.Detailf(
				"cte name %q must be a valid unquoted SQL relation identifier",
				cteName,
			)
		}
		if _, exists := allCTENames[cteName]; exists {
			return ErrFeatureNotAllowed.Detailf("duplicate cte name %q", cteName)
		}
		allCTENames[cteName] = struct{}{}
	}

	availableCTENames := make(map[string]struct{}, len(with.CTEs))
	for _, cte := range with.CTEs {
		cteName := cteRelationName(cte)
		if cte == nil || cte.IsRecursive {
			return ErrFeatureNotAllowed.Detail("recursive CTEs are not allowed")
		}
		if len(cte.ColNameList) > 0 {
			return ErrFeatureNotAllowed.Detailf(
				"cte %q column name lists are not supported",
				cteName,
			)
		}

		cteSelect, err := cteSelectStatement(cte)
		if err != nil {
			return err
		}
		if err := validateCTEDependencyOrder(
			cteName,
			cteSelect,
			allCTENames,
			availableCTENames,
		); err != nil {
			return err
		}
		if err := validateSelectBodySubset(cteSelect, false); err != nil {
			return xerror.Wrapf("cte %q: %w", cteName, err)
		}

		availableCTENames[cteName] = struct{}{}
	}

	return nil
}

// cteRelationName returns the normalized relation name for a CTE definition.
func cteRelationName(cte *ast.CommonTableExpression) string {
	if cte == nil {
		return ""
	}
	return cte.Name.L
}

// cteSelectStatement unwraps a CTE body and rejects non-SELECT bodies such as
// set operations or TABLE statements before runtime materialization can see them.
func cteSelectStatement(cte *ast.CommonTableExpression) (*ast.SelectStmt, error) {
	if cte == nil || cte.Query == nil || cte.Query.Query == nil {
		return nil, ErrFeatureNotAllowed.Detail("cte body must be a single SELECT")
	}

	selectStmt, ok := cte.Query.Query.(*ast.SelectStmt)
	if !ok {
		return nil, ErrFeatureNotAllowed.Detail("cte body must be a single SELECT")
	}

	return selectStmt, nil
}

// validateCTEDependencyOrder rejects recursive, cyclic, and forward CTE
// references while leaving non-CTE relation names for input validation/runtime.
func validateCTEDependencyOrder(
	cteName string,
	selectStmt *ast.SelectStmt,
	allCTENames map[string]struct{},
	availableCTENames map[string]struct{},
) error {
	referencedRelations := collectTransformRelationNames(selectStmt.From)
	for relationName := range referencedRelations {
		if _, isCTE := allCTENames[relationName]; !isCTE {
			continue
		}
		if relationName == cteName {
			return ErrFeatureNotAllowed.Detailf(
				"cte %q cannot reference itself",
				cteName,
			)
		}
		if _, available := availableCTENames[relationName]; !available {
			return ErrFeatureNotAllowed.Detailf(
				"cte %q references cte %q before it is defined",
				cteName,
				relationName,
			)
		}
	}

	return nil
}

// collectTransformRelationNames extracts table relation names from a FROM/JOIN
// tree without interpreting them as declared inputs or CTEs.
func collectTransformRelationNames(from *ast.TableRefsClause) map[string]struct{} {
	relations := make(map[string]struct{})
	if from == nil || from.TableRefs == nil {
		return relations
	}

	collectTransformRelationNamesFromNode(from.TableRefs, relations)
	return relations
}

// collectTransformRelationNamesFromNode recursively collects TableName sources
// used by the supported FROM/JOIN tree.
func collectTransformRelationNamesFromNode(
	node ast.ResultSetNode,
	relations map[string]struct{},
) {
	switch n := node.(type) {
	case *ast.TableSource:
		tableName, ok := n.Source.(*ast.TableName)
		if !ok || tableName == nil {
			return
		}
		relations[tableName.Name.L] = struct{}{}
	case *ast.Join:
		if n.Left != nil {
			collectTransformRelationNamesFromNode(n.Left, relations)
		}
		if n.Right != nil {
			collectTransformRelationNamesFromNode(n.Right, relations)
		}
	}
}

func validateGroupBySubset(selectStmt *ast.SelectStmt) error {
	if selectStmt == nil || selectStmt.GroupBy == nil {
		return nil
	}

	groupedColumns := make(map[string]struct{}, len(selectStmt.GroupBy.Items))
	for _, item := range selectStmt.GroupBy.Items {
		if item == nil {
			continue
		}

		columnExpr, ok := item.Expr.(*ast.ColumnNameExpr)
		if !ok || columnExpr == nil || columnExpr.Name == nil {
			return ErrFeatureNotAllowed.Detail(
				"group by supports only column references",
			)
		}

		groupedColumns[columnExpr.Name.Name.L] = struct{}{}
	}

	if selectStmt.Fields == nil {
		return nil
	}

	for _, field := range selectStmt.Fields.Fields {
		if field == nil || field.Auxiliary {
			continue
		}

		if isTransformAggregateProjectionExpr(field.Expr) {
			continue
		}

		columnExpr, ok := field.Expr.(*ast.ColumnNameExpr)
		if !ok || columnExpr == nil || columnExpr.Name == nil {
			return ErrFeatureNotAllowed.Detail(
				"non-aggregate projection must appear in GROUP BY as a column reference",
			)
		}

		if _, exists := groupedColumns[columnExpr.Name.Name.L]; !exists {
			return ErrFeatureNotAllowed.Detailf(
				"projection column %q must appear in GROUP BY",
				columnExpr.Name.Name.L,
			)
		}
	}

	return nil
}

// validateFromInputRows guarantees the query reads named virtual transform inputs.
func validateFromInputRows(from *ast.TableRefsClause) error {
	if from == nil || from.TableRefs == nil {
		return ErrFeatureNotAllowed.Detail("from clause with input is required")
	}

	return validateTransformFromSource(from.TableRefs)
}

func validateTransformFromSource(node ast.ResultSetNode) error {
	switch n := node.(type) {
	case *ast.TableSource:
		if _, ok := n.Source.(*ast.TableName); !ok {
			return ErrFeatureNotAllowed.Detail("subqueries in from are not allowed")
		}
		return nil
	case *ast.Join:
		if n.Left == nil {
			return ErrFeatureNotAllowed.Detail("join left side is required")
		}
		if err := validateTransformFromSource(n.Left); err != nil {
			return err
		}
		if n.Right != nil {
			if (n.Tp != ast.CrossJoin && n.Tp != ast.LeftJoin) || n.Using != nil || n.NaturalJoin || (n.Tp == ast.LeftJoin && (n.On == nil || n.On.Expr == nil)) {
				return ErrFeatureNotAllowed.Detail("only inner/cross joins and LEFT JOIN with ON conditions are allowed")
			}
			if err := validateTransformFromSource(n.Right); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrFeatureNotAllowed.Detail("unsupported from source")
	}
}

// disallowedNodeVisitor walks the SQL AST to detect features outside the sql_v1 MVP surface
// (subqueries, window functions, set operations, etc.). It implements tidb's ast.Visitor interface.
type disallowedNodeVisitor struct {
	err         error
	projection  bool
	scalarDepth int
}

// Enter short-circuits on the first disallowed node so callers get one clear failure reason.
func (v *disallowedNodeVisitor) Enter(n ast.Node) (ast.Node, bool) {
	if v.err != nil {
		return n, true
	}

	switch node := n.(type) {
	case ast.ValueExpr:
		if literal, ok := node.GetValue().(*test_driver.MyDecimal); ok {
			if _, err := decimal.Parse(literal.String()); err != nil {
				v.err = err
				return n, true
			}
		}
	case *ast.FuncCastExpr:
		if node.Tp != nil && node.Tp.GetType() == mysql.TypeNewDecimal {
			if err := decimal.ValidateDeclaration(node.Tp.GetFlen(), node.Tp.GetDecimal()); err != nil {
				v.err = err
				return n, true
			}
		}
	case *ast.BinaryOperationExpr:
		if v.projection {
			switch node.Op {
			case opcode.Plus, opcode.Minus, opcode.Mul, opcode.Div:
			default:
				v.err = ErrFeatureNotAllowed.Detailf("projection operator %s is not supported", node.Op)
				return n, true
			}
		}
	case *ast.CaseExpr:
		v.err = validateCaseExpression(node)
		return n, true
	case *ast.FuncCallExpr:
		if err := validateNormalizationFunction(node); err != nil {
			v.err = err
			return n, true
		}
		if isNormalizationFunction(node.FnName.L) {
			v.scalarDepth++
		}
	case *ast.SubqueryExpr:
		v.err = ErrFeatureNotAllowed.Detail("subqueries are not allowed")
		return n, true
	case *ast.ExistsSubqueryExpr:
		v.err = ErrFeatureNotAllowed.Detail("exists subquery is not allowed")
		return n, true
	case *ast.PatternInExpr:
		if node.Sel != nil {
			v.err = ErrFeatureNotAllowed.Detail("in subquery is not allowed")
			return n, true
		}
	case *ast.WindowFuncExpr:
		v.err = ErrFeatureNotAllowed.Detail("window functions are not allowed")
		return n, true
	case *ast.SetOprStmt:
		v.err = ErrFeatureNotAllowed.Detail("set operations are not allowed")
		return n, true
	case *ast.AggregateFuncExpr:
		if v.scalarDepth > 0 {
			v.err = ErrFeatureNotAllowed.Detail("aggregates are not supported inside normalization expressions")
			return n, true
		}
		if err := validateAggregateArgumentSubset(node); err != nil {
			v.err = err
			return n, true
		}
	}

	return n, false
}

// Leave continues traversal unless a disallowed node has already been found.
func (v *disallowedNodeVisitor) Leave(n ast.Node) (ast.Node, bool) {
	if fn, ok := n.(*ast.FuncCallExpr); ok && isNormalizationFunction(fn.FnName.L) {
		v.scalarDepth--
	}
	return n, v.err == nil
}

func validateAggregateArgumentSubset(agg *ast.AggregateFuncExpr) error {
	if agg == nil {
		return nil
	}

	fn := strings.ToLower(agg.F)
	if agg.Distinct && fn != ast.AggFuncCount {
		return ErrFeatureNotAllowed.Detailf("DISTINCT is supported only for COUNT, not %s", fn)
	}
	switch fn {
	case ast.AggFuncCount, ast.AggFuncApproxCountDistinct:
		if len(agg.Args) == 0 {
			return nil
		}
		if len(agg.Args) > 1 {
			return ErrFeatureNotAllowed.Detail(
				"count aggregate supports only one argument",
			)
		}

		arg := agg.Args[0]
		if agg.Distinct && !isAggregateColumnArg(arg) {
			return ErrFeatureNotAllowed.Detail("count aggregate supports only column references at runtime")
		}
		if isAggregateColumnArg(arg) || isCountStarLikeArg(arg) {
			return nil
		}

		return ErrFeatureNotAllowed.Detail(
			"count aggregate supports only column references at runtime",
		)
	case ast.AggFuncSum, ast.AggFuncAvg, ast.AggFuncMax, ast.AggFuncMin:
		if len(agg.Args) != 1 {
			return ErrFeatureNotAllowed.Detailf(
				"%s aggregate requires exactly one argument",
				fn,
			)
		}

		arg := agg.Args[0]
		if isAggregateColumnArg(arg) || isNumericCastColumnArg(arg) {
			return nil
		}

		if isCastColumnArg(arg) {
			return ErrFeatureNotAllowed.Detailf(
				"%s aggregate cast argument must target a numeric type",
				fn,
			)
		}

		return ErrFeatureNotAllowed.Detailf(
			"%s aggregate supports only column references or numeric casts at runtime",
			fn,
		)
	}

	return nil
}

func isAggregateColumnArg(expr ast.ExprNode) bool {
	columnExpr, ok := expr.(*ast.ColumnNameExpr)
	return ok && columnExpr != nil && columnExpr.Name != nil
}

func isCountStarLikeArg(expr ast.ExprNode) bool {
	if expr == nil {
		return false
	}

	if strings.TrimSpace(expr.Text()) == "*" {
		return true
	}

	if valueExpr, ok := expr.(ast.ValueExpr); ok {
		literal := strings.TrimSpace(fmt.Sprint(valueExpr.GetValue()))
		return literal == "1" || literal == "*"
	}

	if columnExpr, ok := expr.(*ast.ColumnNameExpr); ok && columnExpr != nil &&
		columnExpr.Name != nil {
		return columnExpr.Name.Name.L == "*" || columnExpr.Name.Name.O == "*"
	}

	return false
}

func isCastColumnArg(expr ast.ExprNode) bool {
	castExpr, ok := expr.(*ast.FuncCastExpr)
	if !ok || castExpr == nil {
		return false
	}

	return isAggregateColumnArg(castExpr.Expr)
}

func isNumericCastColumnArg(expr ast.ExprNode) bool {
	castExpr, ok := expr.(*ast.FuncCastExpr)
	if !ok || castExpr == nil || castExpr.Tp == nil {
		return false
	}

	if !isAggregateColumnArg(castExpr.Expr) {
		return false
	}

	target := strings.ToLower(strings.TrimSpace(castExpr.Tp.String()))
	if target == "" {
		return false
	}

	return strings.Contains(target, "int") ||
		strings.Contains(target, "numeric") ||
		strings.Contains(target, "decimal") ||
		strings.Contains(target, "float") ||
		strings.Contains(target, "double") ||
		strings.Contains(target, "real")
}

// Decimal scale proof is available for literals and explicit casts; schema-only
// field maps and repeating division require a declared contract or final cast.
func decimalProjectionDeclaration(expr ast.ExprNode) (*int, *int) {
	if unary, ok := expr.(*ast.UnaryOperationExpr); ok && (unary.Op == opcode.Plus || unary.Op == opcode.Minus) {
		return decimalProjectionDeclaration(unary.V)
	}
	if cast, ok := expr.(*ast.FuncCastExpr); ok && cast.Tp != nil && cast.Tp.GetType() == mysql.TypeNewDecimal {
		p, s := cast.Tp.GetFlen(), cast.Tp.GetDecimal()
		return &p, &s
	}
	if literal, ok := expr.(ast.ValueExpr); ok {
		if d, ok := literal.GetValue().(*test_driver.MyDecimal); ok {
			text := d.String()
			s := 0
			if index := strings.IndexByte(text, '.'); index >= 0 {
				s = len(text) - index - 1
			}
			if s > decimal.MaxPrecision {
				return nil, nil
			}
			coefficient := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(text, "-"), ".", ""), "0")
			p := max(1, s, len(coefficient))
			return &p, &s
		}
		if inferLiteralType(literal.GetValue()) == model.ContractFieldTypeValueInteger {
			p, s := len(strings.TrimPrefix(fmt.Sprint(literal.GetValue()), "-")), 0
			return &p, &s
		}
	}
	if binary, ok := expr.(*ast.BinaryOperationExpr); ok {
		lp, l := decimalProjectionDeclaration(binary.L)
		rp, r := decimalProjectionDeclaration(binary.R)
		if lp == nil || rp == nil || l == nil || r == nil {
			return nil, nil
		}
		s := max(*l, *r)
		p := max(*lp-*l, *rp-*r) + s + 1
		switch binary.Op {
		case opcode.Plus, opcode.Minus:
		case opcode.Mul:
			s = *l + *r
			p = *lp + *rp
		default:
			return nil, nil
		}
		if s > decimal.MaxPrecision {
			return nil, nil
		}
		p = min(p, decimal.MaxPrecision)
		return &p, &s
	}
	return nil, nil
}
