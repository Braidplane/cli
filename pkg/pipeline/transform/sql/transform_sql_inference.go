package sql

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/opcode"
	"github.com/pingcap/tidb/pkg/parser/test_driver"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/nativetype"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

var nativeDecimalPattern = regexp.MustCompile(`^(?:numeric|bignumeric|decimal|number)\(([0-9]+),\s*([0-9]+)\)$`)

// ContractFieldFromNative retains bounded precision/scale when the provider declares it.
// Unqualified native types keep their existing compatibility mapping.
func ContractFieldFromNative(dataSourceType model.DataSourceType, name, nativeType string) pipepayload.ContractField {
	field := pipepayload.ContractField{Name: name, Type: ContractFieldTypeFromNative(dataSourceType, nativeType)}
	if dataSourceType != model.DataSourceTypePostgres && dataSourceType != model.DataSourceTypeMySQL && dataSourceType != model.DataSourceTypeBigQuery {
		return field
	}
	if len(nativeType) > 128 {
		return field
	}
	match := nativeDecimalPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(nativeType)))
	if match == nil {
		return field
	}
	precision, pe := strconv.Atoi(match[1])
	scale, se := strconv.Atoi(match[2])
	if pe != nil || se != nil || decimal.ValidateDeclaration(precision, scale) != nil {
		return field
	}
	field.Type, field.Precision, field.Scale = model.ContractFieldTypeValueDecimal, &precision, &scale
	return field
}

const (
	aggFuncCount         = "count"
	aggFuncAvg           = "avg"
	aggFuncSum           = "sum"
	aggFuncCountDistinct = "count_distinct"
	jsonFuncObject       = "json_object"
	jsonFuncArray        = "json_array"
	nativeTypeDate       = "date"
	nativeTypeDatetime   = "datetime"
)

type transformInputFieldTypes map[string]model.ContractFieldType

type transformInputRelationAliases map[string]string

type transformRelationScope struct {
	allowedRelations     map[string]struct{}
	allowUnknownRelation bool
}

type transformInputReferenceVisitor struct {
	inputFieldTypes transformInputFieldTypes
	inputAliases    transformInputRelationAliases
	err             error
}

// prepareTransformTypeContext extends the provided input field types with CTE
// output fields, returning the relation scope and aliases needed by the final SELECT.
func prepareTransformTypeContext(
	selectStmt *transformSelect,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
) (transformInputFieldTypes, transformInputRelationAliases, transformRelationScope, error) {
	expandedInputTypes := inputFieldTypes.clone()
	relationScope := transformRelationScope{
		allowedRelations:     inputFieldTypes.declaredRelationNames(),
		allowUnknownRelation: len(inputFieldTypes.declaredRelationNames()) == 0,
	}

	if err := expandCTEFieldTypes(
		selectStmt,
		dataSourceType,
		expandedInputTypes,
		relationScope,
	); err != nil {
		return nil, nil, transformRelationScope{}, err
	}

	inputAliases := expandedInputTypes.relationAliases(selectStmt)
	return expandedInputTypes, inputAliases, relationScope, nil
}

// expandCTEFieldTypes infers each CTE projection in declaration order and
// adds qualified cte.field entries for downstream CTEs and the final SELECT.
func expandCTEFieldTypes(
	selectStmt *transformSelect,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	relationScope transformRelationScope,
) error {
	if selectStmt == nil || selectStmt.With == nil {
		return nil
	}

	selectStmt.typeDetails = make(map[string]transformProjectionTypeDetail)
	declaredInputRelations := copyRelationSet(relationScope.allowedRelations)
	for _, cte := range selectStmt.With.CTEs {
		cteName := cteRelationName(cte)
		if _, shadows := declaredInputRelations[cteName]; shadows {
			return ErrTypeInput.Detailf(
				"cte name %q shadows transform input relation",
				cteName,
			)
		}

		cteSelect, err := cteSelectStatement(cte)
		if err != nil {
			return err
		}

		cteSelect.typeDetails = selectStmt.typeDetails
		cteAliases := inputFieldTypes.relationAliases(cteSelect)
		if err := validateTransformInputReferencesWithRelationScope(
			cteSelect,
			inputFieldTypes,
			cteAliases,
			relationScope,
		); err != nil {
			return xerror.Wrapf("cte %q: %w", cteName, err)
		}
		if _, err := extractProjectionFieldNames(cteSelect); err != nil {
			return xerror.Wrapf("cte %q projection: %w", cteName, err)
		}

		projectionWithTypes, err := extractTransformProjectionWithTypeDetails(
			cteSelect,
			dataSourceType,
			inputFieldTypes,
			cteAliases,
		)
		if err != nil {
			return xerror.Wrapf("cte %q projection type inference: %w", cteName, err)
		}
		for fieldName, detail := range projectionWithTypes {
			inputFieldTypes[cteName+"."+fieldName] = detail.fieldType
			selectStmt.typeDetails[cteName+"."+fieldName] = detail
		}
		relationScope.allowedRelations[cteName] = struct{}{}
	}

	return nil
}

// clone copies field type maps before CTE inference mutates the available relation set.
func (inputFieldTypes transformInputFieldTypes) clone() transformInputFieldTypes {
	cloned := make(transformInputFieldTypes, len(inputFieldTypes))
	for fieldName, fieldType := range inputFieldTypes {
		cloned[fieldName] = fieldType
	}
	return cloned
}

// copyRelationSet clones relation scope maps so CTE shadow checks keep the
// original declared transform inputs separate from later CTE relations.
func copyRelationSet(relations map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(relations))
	for relationName := range relations {
		cloned[relationName] = struct{}{}
	}
	return cloned
}

// validateTransformInputReferencesWithRelationScope is the scoped form used
// after CTE inference has added CTE relations while preserving legacy unqualified
// single-input callers that cannot prove declared relation names.
func validateTransformInputReferencesWithRelationScope(
	selectStmt *transformSelect,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
	relationScope transformRelationScope,
) error {
	if selectStmt != nil && len(selectStmt.unionAll) > 0 {
		for _, branch := range selectStmt.unionAll {
			if err := validateTransformInputReferencesWithRelationScope(branch, inputFieldTypes, inputFieldTypes.relationAliases(branch), relationScope); err != nil {
				return err
			}
		}
		return nil
	}

	if selectStmt == nil || len(inputFieldTypes) == 0 {
		return nil
	}

	if err := validateTransformInputRelations(selectStmt.From, relationScope); err != nil {
		return err
	}

	if selectStmt.Fields != nil {
		for _, field := range selectStmt.Fields.Fields {
			if field == nil || field.Auxiliary {
				continue
			}
			if err := validateTransformInputExpressionReferences(field.Expr, inputFieldTypes, inputAliases); err != nil {
				return err
			}
		}
	}

	if err := validateTransformInputJoinReferences(selectStmt.From, inputFieldTypes, inputAliases); err != nil {
		return err
	}

	if selectStmt.Where != nil {
		if err := validateTransformInputExpressionReferences(selectStmt.Where, inputFieldTypes, inputAliases); err != nil {
			return err
		}
	}

	if selectStmt.GroupBy != nil {
		for _, item := range selectStmt.GroupBy.Items {
			if item == nil {
				continue
			}
			if err := validateTransformInputExpressionReferences(item.Expr, inputFieldTypes, inputAliases); err != nil {
				return err
			}
		}
	}

	if selectStmt.Having != nil {
		aliases := make(map[string]bool)
		for _, field := range selectStmt.Fields.Fields {
			name, err := projectionFieldName(field)
			if err != nil {
				return err
			}
			aliases[name] = true
		}
		visitor := &havingSchemaVisitor{types: inputFieldTypes, aliases: inputAliases, projected: aliases}
		selectStmt.Having.Expr.Accept(visitor)
		if visitor.err != nil {
			return visitor.err
		}
	}

	return nil
}

// validateTransformInputRelations enforces that FROM/JOIN relation names are
// exactly the declared transform.inputs keys available to inference.
func validateTransformInputRelations(
	from *ast.TableRefsClause,
	relationScope transformRelationScope,
) error {
	if from == nil || from.TableRefs == nil {
		return nil
	}

	if len(relationScope.allowedRelations) == 0 || relationScope.allowUnknownRelation {
		return nil
	}

	return validateTransformInputRelationsFromNode(from.TableRefs, relationScope.allowedRelations)
}

func validateTransformInputRelationsFromNode(
	node ast.ResultSetNode,
	declaredRelations map[string]struct{},
) error {
	switch n := node.(type) {
	case *ast.TableSource:
		tableName, ok := n.Source.(*ast.TableName)
		if !ok || tableName == nil {
			return nil
		}
		if _, ok := declaredRelations[tableName.Name.L]; !ok {
			return ErrTypeInput.Detailf("unknown transform input %q", tableName.Name.L)
		}
	case *ast.Join:
		if n.Left != nil {
			if err := validateTransformInputRelationsFromNode(n.Left, declaredRelations); err != nil {
				return err
			}
		}
		if n.Right != nil {
			if err := validateTransformInputRelationsFromNode(n.Right, declaredRelations); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateTransformInputJoinReferences checks JOIN ON expressions separately
// from FROM relation validation because ON expressions are the only part of
// the join tree that can contain column references.
func validateTransformInputJoinReferences(
	from *ast.TableRefsClause,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) error {
	if from == nil || from.TableRefs == nil {
		return nil
	}
	return validateTransformInputJoinReferencesFromNode(from.TableRefs, inputFieldTypes, inputAliases)
}

func validateTransformInputJoinReferencesFromNode(
	node ast.ResultSetNode,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) error {
	if n, ok := node.(*ast.Join); ok {
		if n.Left != nil {
			if err := validateTransformInputJoinReferencesFromNode(n.Left, inputFieldTypes, inputAliases); err != nil {
				return err
			}
		}
		if n.Right != nil {
			if err := validateTransformInputJoinReferencesFromNode(n.Right, inputFieldTypes, inputAliases); err != nil {
				return err
			}
		}
		if n.On != nil && n.On.Expr != nil {
			return validateTransformInputExpressionReferences(n.On.Expr, inputFieldTypes, inputAliases)
		}
	}
	return nil
}

func validateTransformInputExpressionReferences(
	expr ast.ExprNode,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) error {
	if expr == nil {
		return nil
	}
	visitor := &transformInputReferenceVisitor{
		inputFieldTypes: inputFieldTypes,
		inputAliases:    inputAliases,
	}
	_, ok := expr.Accept(visitor)
	if !ok && visitor.err != nil {
		return visitor.err
	}
	return visitor.err
}

// transformInputReferenceVisitor rejects unknown and ambiguous column
// references while allowing non-column expression nodes to be handled by the
// existing SQL subset validator and projection type inference.
func (v *transformInputReferenceVisitor) Enter(n ast.Node) (ast.Node, bool) {
	if v.err != nil {
		return n, true
	}

	columnExpr, ok := n.(*ast.ColumnNameExpr)
	if !ok || columnExpr == nil || columnExpr.Name == nil {
		return n, false
	}

	columnName := columnExpr.Name
	if columnName.Name.L == "*" || columnName.Name.O == "*" {
		return n, false
	}

	_, exists, ambiguous := v.inputFieldTypes.resolveColumnReference(columnName, v.inputAliases)
	if ambiguous {
		v.err = ErrTypeInput.Detailf(
			"ambiguous column reference %q",
			transformColumnReferenceName(columnName),
		)
		return n, true
	}
	if !exists {
		v.err = ErrTypeInput.Detailf(
			"unknown column reference %q",
			transformColumnReferenceName(columnName),
		)
		return n, true
	}

	return n, false
}

func (v *transformInputReferenceVisitor) Leave(n ast.Node) (ast.Node, bool) {
	return n, v.err == nil
}

// inferTransformFieldTypeForDataSource performs best-effort inference for a
// SELECT projection expression over a resolved row contract.
//
// This is a conservative post-load validation pass. When inference is ambiguous,
// it returns Unknown so the caller can reject uncertain contract fulfillment.
func inferTransformFieldTypeForDataSource(
	expr ast.ExprNode,
	dataSourceType model.DataSourceType,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) model.ContractFieldType {
	if expr == nil {
		return model.ContractFieldTypeUnknown
	}

	if !supportsTransformRowInference(dataSourceType) {
		return model.ContractFieldTypeUnknown
	}

	return inferTransformRowFieldType(expr, inputFieldTypes, inputAliases)
}

// supportsTransformRowInference lists datasource results whose resolved payload
// contracts can be exposed to sql_v1 as rows. Connector identity controls native
// schema conversion, not the semantics of the transform expression itself.
func supportsTransformRowInference(dataSourceType model.DataSourceType) bool {
	switch dataSourceType {
	case model.DataSourceTypeUnknown,
		model.DataSourceTypePostgres,
		model.DataSourceTypeMySQL,
		model.DataSourceTypeBigQuery,
		model.DataSourceTypeMongoDB,
		model.DataSourceTypeHTTP,
		model.DataSourceTypeHubSpot,
		model.DataSourceTypeFTP,
		model.DataSourceTypeGit,
		model.DataSourceTypeGithub,
		model.DataSourceTypeGitlab,
		model.DataSourceTypeGoogleWorkspace,
		model.DataSourceTypeS3:
		return true
	default:
		return false
	}
}

// inferTransformRowFieldType infers types from the sql_v1 expression and the
// resolved input row contract. Expressions outside this supported subset remain
// Unknown so output-contract validation fails closed.
func inferTransformRowFieldType(
	expr ast.ExprNode,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) model.ContractFieldType {
	if expr == nil {
		return model.ContractFieldTypeUnknown
	}

	if parens, ok := expr.(*ast.ParenthesesExpr); ok {
		return inferTransformRowFieldType(parens.Expr, inputFieldTypes, inputAliases)
	}
	if window, ok := expr.(*ast.WindowFuncExpr); ok {
		items := append([]*ast.ByItem(nil), window.Spec.OrderBy.Items...)
		if window.Spec.PartitionBy != nil {
			items = append(items, window.Spec.PartitionBy.Items...)
		}
		for _, item := range items {
			if !supportedWindowKeyType(inferTransformRowFieldType(item.Expr, inputFieldTypes, inputAliases)) {
				return model.ContractFieldTypeUnknown
			}
		}
		return model.ContractFieldTypeValueInteger
	}

	if caseExpr, ok := expr.(*ast.CaseExpr); ok {
		args := []ast.ExprNode{caseExpr.ElseClause}
		for _, clause := range caseExpr.WhenClauses {
			args = append(args, clause.Result)
		}
		return inferNormalizationType(args, inputFieldTypes, inputAliases)
	}

	if castExpr, ok := expr.(*ast.FuncCastExpr); ok && castExpr != nil {
		if inferTransformRowFieldType(castExpr.Expr, inputFieldTypes, inputAliases) ==
			model.ContractFieldTypeUnknown || castExpr.Tp == nil {
			return model.ContractFieldTypeUnknown
		}
		return contractFieldTypeFromTransformCast(castExpr.Tp.String())
	}

	if unary, ok := expr.(*ast.UnaryOperationExpr); ok && (unary.Op == opcode.Plus || unary.Op == opcode.Minus) {
		if inferTransformRowFieldType(unary.V, inputFieldTypes, inputAliases) == model.ContractFieldTypeValueDecimal {
			return model.ContractFieldTypeValueDecimal
		}
		return model.ContractFieldTypeUnknown
	}
	if valueExpr, ok := expr.(ast.ValueExpr); ok {
		return inferLiteralType(valueExpr.GetValue())
	}

	if binaryExpr, ok := expr.(*ast.BinaryOperationExpr); ok && binaryExpr != nil {
		leftType := inferTransformRowFieldType(binaryExpr.L, inputFieldTypes, inputAliases)
		rightType := inferTransformRowFieldType(binaryExpr.R, inputFieldTypes, inputAliases)
		switch binaryExpr.Op {
		case opcode.Plus, opcode.Minus, opcode.Mul, opcode.Div:
			// NULL is a nullable numeric operand, not an unknown schema type.
			if literal, ok := binaryExpr.L.(ast.ValueExpr); ok && literal.GetValue() == nil {
				leftType = model.ContractFieldTypeValueNumber
			}
			if literal, ok := binaryExpr.R.(ast.ValueExpr); ok && literal.GetValue() == nil {
				rightType = model.ContractFieldTypeValueNumber
			}
		default:
			return model.ContractFieldTypeUnknown
		}
		if isNumericBinaryExpression(leftType, rightType) {
			if leftType == model.ContractFieldTypeValueDecimal || rightType == model.ContractFieldTypeValueDecimal {
				if (leftType == model.ContractFieldTypeValueNumber && !isNullLiteral(binaryExpr.L)) || (rightType == model.ContractFieldTypeValueNumber && !isNullLiteral(binaryExpr.R)) {
					return model.ContractFieldTypeUnknown
				}
				return model.ContractFieldTypeValueDecimal
			}
			return model.ContractFieldTypeValueNumber
		}
		return model.ContractFieldTypeUnknown
	}

	if agg, ok := expr.(*ast.AggregateFuncExpr); ok && agg != nil {
		return inferTransformAggregateFieldType(
			agg.F,
			agg.Args,
			inputFieldTypes,
			inputAliases,
		)
	}

	// Only try to infer from function calls where the type is deterministic.
	if fn, ok := expr.(*ast.FuncCallExpr); ok && fn != nil && fn.FnName.L != "" {
		fnName := strings.ToLower(fn.FnName.L)
		switch fnName {
		case "coalesce":
			return inferNormalizationType(fn.Args, inputFieldTypes, inputAliases)
		case "nullif":
			return inferTransformRowFieldType(fn.Args[0], inputFieldTypes, inputAliases)
		case "trim", "lower", "upper", "str_to_date", "date_format":
			if inferTransformRowFieldType(fn.Args[0], inputFieldTypes, inputAliases) == model.ContractFieldTypeValueString || isNullLiteral(fn.Args[0]) {
				return model.ContractFieldTypeValueString
			}
			return model.ContractFieldTypeUnknown
		case aggFuncCount, aggFuncCountDistinct, aggFuncSum, aggFuncAvg, ast.AggFuncMax, ast.AggFuncMin:
			return inferTransformAggregateFieldType(
				fnName,
				fn.Args,
				inputFieldTypes,
				inputAliases,
			)
		case jsonFuncObject:
			return model.ContractFieldTypeValueObject
		case jsonFuncArray:
			return model.ContractFieldTypeValueArray
		}
	}

	if colExpr, ok := expr.(*ast.ColumnNameExpr); ok && colExpr != nil && colExpr.Name != nil {
		if inferred, ok, _ := inputFieldTypes.resolveColumnReference(colExpr.Name, inputAliases); ok {
			return inferred
		}
	}

	return model.ContractFieldTypeUnknown
}

func inferTransformAggregateFieldType(
	functionName string,
	args []ast.ExprNode,
	inputFieldTypes transformInputFieldTypes,
	inputAliases transformInputRelationAliases,
) model.ContractFieldType {
	switch strings.ToLower(functionName) {
	case aggFuncCount, aggFuncCountDistinct, ast.AggFuncApproxCountDistinct:
		if len(args) == 1 && isCountStarLikeArg(args[0]) {
			return model.ContractFieldTypeValueInteger
		}
		if len(args) != 1 || !isAggregateColumnArg(args[0]) ||
			inferTransformRowFieldType(args[0], inputFieldTypes, inputAliases) ==
				model.ContractFieldTypeUnknown {
			return model.ContractFieldTypeUnknown
		}
		return model.ContractFieldTypeValueInteger
	case aggFuncSum, aggFuncAvg, ast.AggFuncMax, ast.AggFuncMin:
		if len(args) != 1 ||
			(!isAggregateColumnArg(args[0]) && !isNumericCastColumnArg(args[0])) ||
			!isNumericTransformType(
				inferTransformRowFieldType(args[0], inputFieldTypes, inputAliases),
			) {
			return model.ContractFieldTypeUnknown
		}
		if inferTransformRowFieldType(args[0], inputFieldTypes, inputAliases) == model.ContractFieldTypeValueDecimal {
			return model.ContractFieldTypeValueDecimal
		}
		return model.ContractFieldTypeValueNumber
	default:
		return model.ContractFieldTypeUnknown
	}
}

// contractFieldTypeFromTransformCast mirrors the cast targets implemented by
// sql_v1 runtime. These types describe transform output and are independent of
// the connector that produced the input rows.
func contractFieldTypeFromTransformCast(target string) model.ContractFieldType {
	target = strings.ToLower(strings.TrimSpace(target))
	switch {
	case strings.Contains(target, "char"),
		strings.Contains(target, "text"),
		strings.Contains(target, "string"):
		return model.ContractFieldTypeValueString
	case strings.Contains(target, "int"):
		return model.ContractFieldTypeValueInteger
	case strings.Contains(target, "decimal"), strings.Contains(target, "numeric"):
		return model.ContractFieldTypeValueDecimal
	case
		strings.Contains(target, "float"),
		strings.Contains(target, "double"),
		strings.Contains(target, "real"):
		return model.ContractFieldTypeValueNumber
	default:
		return model.ContractFieldTypeUnknown
	}
}

func inferLiteralType(value any) model.ContractFieldType {
	switch value.(type) {
	case *test_driver.MyDecimal:
		return model.ContractFieldTypeValueDecimal
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return model.ContractFieldTypeValueInteger
	case float32, float64:
		return model.ContractFieldTypeValueNumber
	case string:
		return model.ContractFieldTypeValueString
	case bool:
		return model.ContractFieldTypeValueBoolean
	default:
		return model.ContractFieldTypeUnknown
	}
}

func isNumericBinaryExpression(
	left model.ContractFieldType,
	right model.ContractFieldType,
) bool {
	return isNumericTransformType(left) && isNumericTransformType(right)
}

func isNumericTransformType(t model.ContractFieldType) bool {
	return t == model.ContractFieldTypeValueInteger ||
		t == model.ContractFieldTypeValueNumber || t == model.ContractFieldTypeValueDecimal
}

// ContractFieldTypeFromNative maps datasource-native field type strings
// from schema introspection into the unified contract field type.
func ContractFieldTypeFromNative(
	dataSourceType model.DataSourceType,
	nativeType string,
) model.ContractFieldType {
	n := strings.ToLower(strings.TrimSpace(nativeType))
	if dataSourceType == model.DataSourceTypeBigQuery && (strings.HasPrefix(n, "numeric(") || strings.HasPrefix(n, "bignumeric(")) {
		return model.ContractFieldTypeValueString
	}
	if n == "" {
		return model.ContractFieldTypeUnknown
	}

	switch dataSourceType {
	case model.DataSourceTypePostgres, model.DataSourceTypeMySQL:
		switch {
		case strings.Contains(n, "int"):
			return model.ContractFieldTypeValueInteger
		case strings.Contains(n, "numeric") || strings.Contains(n, "decimal") || strings.Contains(n, "float") || strings.Contains(n, "double") || strings.Contains(n, "real"):
			return model.ContractFieldTypeValueNumber
		case strings.Contains(n, "bool"):
			return model.ContractFieldTypeValueBoolean
		case strings.Contains(n, "json"):
			return model.ContractFieldTypeValueObject
		case strings.Contains(n, nativeTypeDate) || strings.Contains(n, "time"):
			return model.ContractFieldTypeValueDatetime
		case strings.Contains(n, "char") || strings.Contains(n, "text") || strings.Contains(n, "uuid") || strings.Contains(n, "string"):
			return model.ContractFieldTypeValueString
		default:
			return model.ContractFieldTypeUnknown
		}
	case model.DataSourceTypeBigQuery:
		switch n {
		case "int64", "integer":
			return model.ContractFieldTypeValueInteger
		case "float64", "float":
			return model.ContractFieldTypeValueNumber
		case "bool", "boolean":
			return model.ContractFieldTypeValueBoolean
		case "json", "record", "struct":
			return model.ContractFieldTypeValueObject
		case "string", nativeTypeDate, nativeTypeDatetime, "time", "timestamp", "geography", "bytes", "numeric", "bignumeric":
			return model.ContractFieldTypeValueString
		default:
			return model.ContractFieldTypeUnknown
		}
	case model.DataSourceTypeMongoDB:
		switch n {
		case "string", "objectid":
			return model.ContractFieldTypeValueString
		case "int", "int32", "int64", "long":
			return model.ContractFieldTypeValueInteger
		case "double", "decimal", "decimal128", "number":
			return model.ContractFieldTypeValueNumber
		case "bool", "boolean":
			return model.ContractFieldTypeValueBoolean
		case "object", "document":
			return model.ContractFieldTypeValueObject
		case "array":
			return model.ContractFieldTypeValueArray
		case nativeTypeDate, nativeTypeDatetime, "timestamp":
			return model.ContractFieldTypeValueDatetime
		default:
			return model.ContractFieldTypeUnknown
		}
	case model.DataSourceTypeHTTP:
		return contractTypeFromJSONLikeNative(n)
	case model.DataSourceTypeHubSpot:
		switch n {
		case "enumeration":
			return model.ContractFieldTypeValueString
		case nativeTypeDate, nativeTypeDatetime:
			return model.ContractFieldTypeValueDatetime
		default:
			return contractTypeFromJSONLikeNative(n)
		}
	case model.DataSourceTypeS3,
		model.DataSourceTypeFTP,
		model.DataSourceTypeGit,
		model.DataSourceTypeGithub,
		model.DataSourceTypeGitlab:
		return contractTypeFromJSONLikeNative(n)
	default:
		return model.ContractFieldTypeUnknown
	}
}

func contractTypeFromJSONLikeNative(native string) model.ContractFieldType {
	switch nativetype.NormalizeJSONLike(native) {
	case nativetype.TypeString:
		return model.ContractFieldTypeValueString
	case nativetype.TypeInteger:
		return model.ContractFieldTypeValueInteger
	case nativetype.TypeNumber:
		return model.ContractFieldTypeValueNumber
	case nativetype.TypeBoolean:
		return model.ContractFieldTypeValueBoolean
	case nativetype.TypeObject:
		return model.ContractFieldTypeValueObject
	case nativetype.TypeArray:
		return model.ContractFieldTypeValueArray
	case nativetype.TypeDatetime:
		return model.ContractFieldTypeValueDatetime
	default:
		return model.ContractFieldTypeUnknown
	}
}

// BuildTransformInputFieldTypes normalizes introspected field types for input.
func BuildTransformInputFieldTypes(
	dataSourceType model.DataSourceType,
	nativeFieldTypes map[string]string,
) map[string]model.ContractFieldType {
	result := make(map[string]model.ContractFieldType, len(nativeFieldTypes))
	for fieldName, nativeType := range nativeFieldTypes {
		result[fieldName] = ContractFieldTypeFromNative(dataSourceType, nativeType)
	}

	return result
}

func (inputFieldTypes transformInputFieldTypes) declaredRelationNames() map[string]struct{} {
	relations := make(map[string]struct{})
	for fieldName := range inputFieldTypes {
		relationName, _, ok := strings.Cut(fieldName, ".")
		if !ok || relationName == "" {
			continue
		}
		relations[relationName] = struct{}{}
	}
	return relations
}

func (inputFieldTypes transformInputFieldTypes) relationAliases(
	selectStmt *transformSelect,
) transformInputRelationAliases {
	if selectStmt == nil || selectStmt.From == nil || selectStmt.From.TableRefs == nil ||
		len(inputFieldTypes) == 0 {
		return nil
	}

	aliases := make(transformInputRelationAliases)
	aliases.addRelationAliasesFromNode(selectStmt.From.TableRefs)
	return aliases
}

// addRelationAliasesFromNode records SQL table aliases without adding them to
// the declared input set. Unknown FROM/JOIN relations must still fail against
// the original transform.inputs names before alias-aware column lookup runs.
func (aliases transformInputRelationAliases) addRelationAliasesFromNode(
	node ast.ResultSetNode,
) {
	switch n := node.(type) {
	case *ast.TableSource:
		tableName, ok := n.Source.(*ast.TableName)
		if !ok || tableName == nil {
			return
		}
		aliases[tableName.Name.L] = tableName.Name.L
		if n.AsName.L != "" {
			aliases[n.AsName.L] = tableName.Name.L
		}
	case *ast.Join:
		if n.Left != nil {
			aliases.addRelationAliasesFromNode(n.Left)
		}
		if n.Right != nil {
			aliases.addRelationAliasesFromNode(n.Right)
		}
	}
}

// visibleRelationNames returns the real relation names visible in one SELECT
// body after SQL aliases are resolved.
func (inputAliases transformInputRelationAliases) visibleRelationNames() map[string]struct{} {
	if len(inputAliases) == 0 {
		return nil
	}

	relations := make(map[string]struct{}, len(inputAliases))
	for _, relationName := range inputAliases {
		relations[relationName] = struct{}{}
	}
	return relations
}

// resolveColumnReference implements named-input lookup. Qualified references
// may use the declared input name or a SQL alias for that input; unqualified
// references are allowed only when one declared input relation contains the field.
func (inputFieldTypes transformInputFieldTypes) resolveColumnReference(
	columnName *ast.ColumnName,
	inputAliases transformInputRelationAliases,
) (model.ContractFieldType, bool, bool) {
	if columnName == nil {
		return model.ContractFieldTypeUnknown, false, false
	}

	if columnName.Table.L != "" {
		referenceName := transformColumnReferenceName(columnName)
		if inferred, ok := inputFieldTypes[referenceName]; ok {
			return inferred, true, false
		}
		if relationName, ok := inputAliases[columnName.Table.L]; ok {
			referenceName = relationName + "." + columnName.Name.L
			if inferred, ok := inputFieldTypes[referenceName]; ok {
				return inferred, true, false
			}
			if inferred, ok := inputFieldTypes[columnName.Name.L]; ok {
				return inferred, true, false
			}
		}
		return model.ContractFieldTypeUnknown, false, false
	}

	suffix := "." + columnName.Name.L
	visibleRelations := inputAliases.visibleRelationNames()
	var inferred model.ContractFieldType
	matches := 0
	for fieldName, fieldType := range inputFieldTypes {
		if strings.HasSuffix(fieldName, suffix) {
			if len(visibleRelations) > 0 {
				relationName, _, ok := strings.Cut(fieldName, ".")
				if !ok {
					continue
				}
				if _, visible := visibleRelations[relationName]; !visible {
					continue
				}
			}
			matches++
			inferred = fieldType
		}
	}
	if matches == 1 {
		return inferred, true, false
	}
	if matches > 1 {
		return model.ContractFieldTypeUnknown, false, true
	}

	if inferred, ok := inputFieldTypes[columnName.Name.L]; ok {
		return inferred, true, false
	}

	return model.ContractFieldTypeUnknown, false, false
}

func isNullLiteral(expr ast.ExprNode) bool {
	literal, ok := expr.(ast.ValueExpr)
	return ok && literal.GetValue() == nil
}
