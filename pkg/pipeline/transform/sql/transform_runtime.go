package sql

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/opcode"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	domainvalidate "github.com/Braidplane/cli/pkg/structvalidate"
	"github.com/Braidplane/cli/pkg/xerror"
)

type transformOperationKind int

const (
	transformOperationKindUnknown transformOperationKind = iota // unknown
	transformOperationKindSQLV1                                 // sql_v1
)

type sqlV1TransformOperationConfig struct {
	SQL         string
	OutputKind  model.TransformOutputKind
	InputSchema *pipepayload.Contract
}

type decodedTransformOperationConfig struct {
	kind  transformOperationKind
	sqlV1 *sqlV1TransformOperationConfig
}

type rowInput struct {
	Rows        []map[string]any
	ColumnNames []string
	RowCount    int
}

type transformJSONConstructedValue struct {
	value any
}

// newTransformJSONConstructedValue marks JSON helper output so serializers can
// preserve native values for JSON documents while keeping rows/csv string output.
func newTransformJSONConstructedValue(value any) transformJSONConstructedValue {
	return transformJSONConstructedValue{value: value}
}

// MarshalJSON writes constructed JSON helpers as native objects or arrays when
// a transform serializes rows into output_kind=json documents.
func (v transformJSONConstructedValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(nativeTransformJSONValue(v.value))
}

// String renders constructed JSON helpers as encoded JSON for row-oriented
// outputs that expose one value per projected column.
func (v transformJSONConstructedValue) String() string {
	encoded, err := json.Marshal(nativeTransformJSONValue(v.value))
	if err != nil {
		return fmt.Sprint(v.value)
	}
	return string(encoded)
}

// nativeTransformJSONValue recursively unwraps constructed helper values so
// nested JSON_OBJECT and JSON_ARRAY calls compose as native JSON structures.
func nativeTransformJSONValue(value any) any {
	switch v := value.(type) {
	case transformJSONConstructedValue:
		return nativeTransformJSONValue(v.value)
	case map[string]any:
		native := make(map[string]any, len(v))
		for key, nested := range v {
			native[key] = nativeTransformJSONValue(nested)
		}
		return native
	case []any:
		native := make([]any, len(v))
		for i, nested := range v {
			native[i] = nativeTransformJSONValue(nested)
		}
		return native
	default:
		return value
	}
}

// rowTransformJSONValue recursively stringifies constructed helper values for
// row and csv outputs without changing ordinary nested input values.
func rowTransformJSONValue(value any) any {
	switch v := value.(type) {
	case transformJSONConstructedValue:
		return v.String()
	case map[string]any:
		rowValue := make(map[string]any, len(v))
		for key, nested := range v {
			rowValue[key] = rowTransformJSONValue(nested)
		}
		return rowValue
	case []any:
		rowValue := make([]any, len(v))
		for i, nested := range v {
			rowValue[i] = rowTransformJSONValue(nested)
		}
		return rowValue
	default:
		return value
	}
}

// rowsWithRowOrientedJSONValues prepares transform rows for representations
// where each projection column remains a scalar-ish row field.
func rowsWithRowOrientedJSONValues(rows []map[string]any) []map[string]any {
	converted := make([]map[string]any, len(rows))
	for i, row := range rows {
		convertedRow := make(map[string]any, len(row))
		for key, value := range row {
			convertedRow[key] = rowTransformJSONValue(value)
		}
		converted[i] = convertedRow
	}
	return converted
}

func executeTransformStepFromPayloads(
	transform *Transform,
	upstreamPayloads map[string]*pipepayload.Payload,
) (*pipepayload.Output, error) {
	return executeTransformStepFromPayloadsWithBudget(
		transform,
		upstreamPayloads,
		newExecutionBudget(context.Background(), Limits{}),
	)
}

func executeTransformStepFromPayloadsWithBudget(
	transform *Transform,
	upstreamPayloads map[string]*pipepayload.Payload,
	budget *executionBudget,
) (*pipepayload.Output, error) {
	if err := budget.checkContext(); err != nil {
		return nil, err
	}
	decodedConfig, err := decodeTransformOperationConfig(transform)
	if err != nil {
		return nil, err
	}
	if len(upstreamPayloads) == 0 {
		return nil, xerror.UnexpectedUpstreamResultRequired()
	}

	if decodedConfig.kind != transformOperationKindSQLV1 || decodedConfig.sqlV1 == nil {
		return nil, xerror.Unsupported(xerror.Pipeline).Detailf(
			"unsupported transform operation kind %d", decodedConfig.kind,
		)
	}

	inputSQL := make(map[string]*rowInput, len(upstreamPayloads))
	names := make([]string, 0, len(upstreamPayloads))
	for name := range upstreamPayloads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		upstreamPayload := upstreamPayloads[name]
		if upstreamPayload == nil {
			return nil, xerror.UnexpectedUpstreamResultRequired()
		}
		resolvedInput, err := runtimePayloadToTransformInputWithBudget(
			upstreamPayload,
			contractForNamedTransformInput(
				decodedConfig.sqlV1.InputSchema,
				name,
				len(upstreamPayloads),
			),
			budget,
		)
		if err != nil {
			return nil, err
		}
		inputSQL[name] = resolvedInput
	}

	return executeTransformStepWithInputs(decodedConfig, inputSQL, budget)
}

func executeTransformStepWithInputs(
	decodedConfig *decodedTransformOperationConfig,
	inputSQL map[string]*rowInput,
	budget *executionBudget,
) (*pipepayload.Output, error) {
	if decodedConfig == nil || decodedConfig.sqlV1 == nil {
		return nil, xerror.Unsupported(
			xerror.Pipeline,
		).Detail(
			"unsupported transform operation config",
		)
	}

	selectStmt, _, err := parseTransformSelect(decodedConfig.sqlV1.SQL)
	if err != nil {
		return nil, err
	}

	relationInputs, err := materializeTransformCTEs(selectStmt.With, inputSQL, budget, 1)
	if err != nil {
		return nil, err
	}

	evaluated, err := evaluateTransformSelectRows(selectStmt, relationInputs, budget, 1)
	if err != nil {
		return nil, err
	}

	return buildTransformExecutionResult(
		decodedConfig.sqlV1.OutputKind,
		evaluated.Rows,
		evaluated.ColumnNames,
		budget,
	)
}

// materializeTransformCTEs evaluates each non-recursive CTE in declaration
// order and exposes it as another in-memory row relation for downstream SELECTs.
func materializeTransformCTEs(
	with *ast.WithClause,
	inputSQL map[string]*rowInput,
	budget *executionBudget,
	depth int,
) (map[string]*rowInput, error) {
	if err := budget.checkDepth(depth); err != nil {
		return nil, err
	}
	relationInputs := make(map[string]*rowInput, len(inputSQL))
	for name, input := range inputSQL {
		relationInputs[name] = input
	}

	if with == nil {
		return relationInputs, nil
	}

	for _, cte := range with.CTEs {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		cteName := cteRelationName(cte)
		if _, exists := relationInputs[cteName]; exists {
			return nil, ErrFeatureNotAllowed.Detailf(
				"cte name %q shadows an existing transform relation",
				cteName,
			)
		}

		cteSelect, err := cteSelectStatement(cte)
		if err != nil {
			return nil, err
		}

		evaluated, err := evaluateTransformSelectRows(cteSelect, relationInputs, budget, depth+1)
		if err != nil {
			return nil, xerror.Wrapf("materialize cte %q: %w", cteName, err)
		}
		relationInputs[cteName] = evaluated
	}

	return relationInputs, nil
}

// evaluateTransformSelectRows executes one already-validated SELECT body over
// the provided row relations and returns its projected row relation.
func evaluateTransformSelectRows(
	selectStmt *ast.SelectStmt,
	inputSQL map[string]*rowInput,
	budget *executionBudget,
	depth int,
) (*rowInput, error) {
	if err := budget.checkDepth(depth); err != nil {
		return nil, err
	}
	inputRows, err := buildTransformInputRows(selectStmt.From, inputSQL, budget, depth)
	if err != nil {
		return nil, err
	}

	filteredRows, err := applyWhereFilter(selectStmt, inputRows, budget)
	if err != nil {
		return nil, xerror.Wrapf("apply transform where filter: %w", err)
	}

	projectedRows := make([]map[string]any, 0, len(filteredRows))
	columnNames := make([]string, 0, len(selectStmt.Fields.Fields))
	hasAggregate := false

	for _, field := range selectStmt.Fields.Fields {
		if field.Auxiliary {
			continue
		}
		if isTransformAggregateProjectionExpr(field.Expr) {
			hasAggregate = true
		}
		name, err := projectionFieldName(field)
		if err != nil {
			return nil, err
		}
		columnNames = append(columnNames, name)
	}

	if hasAggregate || selectStmt.GroupBy != nil {
		groupedRows, err := evaluateTransformGroupedRows(selectStmt, filteredRows, budget)
		if err != nil {
			return nil, err
		}

		finalRows, err := applyOrderByAndLimit(selectStmt, groupedRows, budget)
		if err != nil {
			return nil, xerror.Wrapf("apply transform ORDER BY/LIMIT: %w", err)
		}

		return &rowInput{
			Rows:        finalRows,
			ColumnNames: columnNames,
			RowCount:    len(finalRows),
		}, nil
	}

	for _, row := range filteredRows {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		projected := make(map[string]any, len(columnNames))
		for _, field := range selectStmt.Fields.Fields {
			if field.Auxiliary {
				continue
			}

			name, err := projectionFieldName(field)
			if err != nil {
				return nil, err
			}
			value, err := evaluateTransformProjectionExpr(field.Expr, row, budget, 1)
			if err != nil {
				return nil, err
			}
			projected[name] = value
		}
		projectedSize, err := estimateTransformRowBytes(projected, budget, 1)
		if err != nil {
			return nil, err
		}
		if err := budget.chargeIntermediate(1, projectedSize); err != nil {
			return nil, err
		}
		projectedRows = append(projectedRows, projected)
	}

	finalRows, err := applyOrderByAndLimit(selectStmt, projectedRows, budget)
	if err != nil {
		return nil, xerror.Wrapf("apply transform ORDER BY/LIMIT: %w", err)
	}

	return &rowInput{
		Rows:        finalRows,
		ColumnNames: columnNames,
		RowCount:    len(finalRows),
	}, nil
}

func buildTransformInputRows(
	from *ast.TableRefsClause,
	inputs map[string]*rowInput,
	budget *executionBudget,
	depth int,
) ([]map[string]any, error) {
	if from == nil || from.TableRefs == nil {
		return nil, ErrFeatureNotAllowed.Detail("from clause with input is required")
	}
	return buildTransformInputRowsFromNode(from.TableRefs, inputs, budget, depth)
}

func buildTransformInputRowsFromNode(
	node ast.ResultSetNode,
	inputs map[string]*rowInput,
	budget *executionBudget,
	depth int,
) ([]map[string]any, error) {
	if err := budget.checkDepth(depth); err != nil {
		return nil, err
	}
	switch n := node.(type) {
	case *ast.TableSource:
		tableName, ok := n.Source.(*ast.TableName)
		if !ok || tableName == nil {
			return nil, ErrFeatureNotAllowed.Detail("subqueries in from are not allowed")
		}
		inputName := tableName.Name.L
		input, ok := inputs[inputName]
		if !ok || input == nil {
			return nil, ErrFeatureNotAllowed.Detailf("from references unknown transform input %q", inputName)
		}
		qualifier := inputName
		if n.AsName.L != "" {
			qualifier = n.AsName.L
		}
		return qualifiedTransformInputRows(input.Rows, qualifier, budget)
	case *ast.Join:
		leftRows, err := buildTransformInputRowsFromNode(n.Left, inputs, budget, depth+1)
		if err != nil {
			return nil, err
		}
		if n.Right == nil {
			return leftRows, nil
		}
		if n.Tp != ast.CrossJoin || n.Using != nil || n.NaturalJoin {
			return nil, ErrFeatureNotAllowed.Detail("only inner joins with ON conditions are allowed")
		}
		rightRows, err := buildTransformInputRowsFromNode(n.Right, inputs, budget, depth+1)
		if err != nil {
			return nil, err
		}
		candidateCount, err := checkedProduct(len(leftRows), len(rightRows))
		if err != nil {
			return nil, err
		}
		if err := budget.chargeWork(candidateCount); err != nil {
			return nil, err
		}
		capacity := candidateCount
		const boundedJoinPreallocation = int64(1024)
		if capacity > boundedJoinPreallocation {
			capacity = boundedJoinPreallocation
		}
		if budget.limits.MaxIntermediateRows > 0 {
			remaining := int64(budget.limits.MaxIntermediateRows) - budget.intermediateRows
			if remaining < capacity {
				capacity = max(remaining, 0)
			}
		}
		joinedRows := make([]map[string]any, 0, int(capacity))
		for _, leftRow := range leftRows {
			for _, rightRow := range rightRows {
				if err := budget.checkContext(); err != nil {
					return nil, err
				}
				candidateSize, err := estimateMergedTransformRowBytes(leftRow, rightRow, budget)
				if err != nil {
					return nil, err
				}
				if err := budget.chargeIntermediate(1, candidateSize); err != nil {
					return nil, err
				}
				joinedRow := mergeTransformRows(leftRow, rightRow)
				if n.On != nil && n.On.Expr != nil {
					keep, err := evaluateWhereExpression(n.On.Expr, joinedRow, budget, 1)
					if err != nil {
						return nil, xerror.Wrapf("evaluate JOIN ON expression: %w", err)
					}
					if !keep {
						continue
					}
				}
				joinedRows = append(joinedRows, joinedRow)
			}
		}
		return joinedRows, nil
	default:
		return nil, ErrFeatureNotAllowed.Detail("unsupported from source")
	}
}

func qualifiedTransformInputRows(
	rows []map[string]any,
	qualifier string,
	budget *executionBudget,
) ([]map[string]any, error) {
	qualifiedRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		qualifiedSize, err := estimateQualifiedTransformRowBytes(row, qualifier, budget)
		if err != nil {
			return nil, err
		}
		if err := budget.chargeIntermediate(1, qualifiedSize); err != nil {
			return nil, err
		}
		qualifiedRow := make(map[string]any, len(row)*2)
		for fieldName, value := range row {
			if _, exists := qualifiedRow[fieldName]; !exists {
				qualifiedRow[fieldName] = value
			}
			qualifiedRow[qualifier+"."+fieldName] = value
		}
		qualifiedRows = append(qualifiedRows, qualifiedRow)
	}
	return qualifiedRows, nil
}

func mergeTransformRows(left, right map[string]any) map[string]any {
	merged := make(map[string]any, len(left)+len(right))
	for fieldName, value := range left {
		merged[fieldName] = value
	}
	for fieldName, value := range right {
		existingKey, exists := rowFieldKey(merged, fieldName)
		if exists {
			ambiguous := ambiguousTransformFieldValue{fieldName: fieldName}
			merged[existingKey] = ambiguous
			if existingKey != fieldName {
				merged[fieldName] = ambiguous
			}
			continue
		}
		merged[fieldName] = value
	}
	return merged
}

func decodeTransformOperationConfig(
	transform *Transform,
) (*decodedTransformOperationConfig, error) {
	if transform == nil {
		return nil, xerror.UnexpectedTransformSpecRequired()
	}

	if strings.TrimSpace(transform.SQL) == "" {
		return nil, ErrRequired.Detail("decode transform operation config")
	}

	outputKind := transform.OutputKind
	if outputKind == model.TransformOutputKindUnknown {
		outputKind = model.TransformOutputKindRows
	}
	if !domainvalidate.ValidTransformOutputKind(outputKind) {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detailf(
			"decode transform operation config: unsupported output_kind value %d",
			transform.OutputKind,
		)
	}

	return &decodedTransformOperationConfig{
		kind: transformOperationKindSQLV1,
		sqlV1: &sqlV1TransformOperationConfig{
			SQL:         transform.SQL,
			OutputKind:  outputKind,
			InputSchema: transform.InputContract,
		},
	}, nil
}

func contractForNamedTransformInput(
	contract *pipepayload.Contract,
	inputName string,
	inputCount int,
) *pipepayload.Contract {
	if contract == nil || inputCount <= 1 {
		return contract
	}

	prefix := inputName + "."
	fields := make([]pipepayload.ContractField, 0, len(contract.Fields))
	for _, field := range contract.Fields {
		if !strings.HasPrefix(field.Name, prefix) {
			continue
		}
		field.Name = strings.TrimPrefix(field.Name, prefix)
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return nil
	}

	namedContract := *contract
	namedContract.Fields = fields
	return &namedContract
}

func runtimePayloadToTransformInput(
	upstreamPayload *pipepayload.Payload,
	contract *pipepayload.Contract,
) (*rowInput, error) {
	return runtimePayloadToTransformInputWithBudget(
		upstreamPayload,
		contract,
		newExecutionBudget(context.Background(), Limits{}),
	)
}

func runtimePayloadToTransformInputWithBudget(
	upstreamPayload *pipepayload.Payload,
	contract *pipepayload.Contract,
	budget *executionBudget,
) (*rowInput, error) {
	if err := budget.checkContext(); err != nil {
		return nil, err
	}
	if upstreamPayload == nil {
		return nil, xerror.UnexpectedUpstreamResultRequired()
	}

	switch upstreamPayload.Representation {
	case model.ContractRepresentationRows:
		validationRows := pipepayload.CoerceContractFieldValidationRows(contract, upstreamPayload.Rows)
		if err := validateTransformInputContract(
			validationRows,
			contract,
			"runtime payload",
		); err != nil {
			return nil, err
		}
		input := &rowInput{
			Rows:        upstreamPayload.Rows,
			ColumnNames: upstreamPayload.ColumnNames,
			RowCount:    upstreamPayload.RowCount,
		}
		if err := chargeInputRows(input.Rows, budget); err != nil {
			return nil, err
		}
		return input, nil
	case model.ContractRepresentationJSON:
		if len(upstreamPayload.Document) == 0 {
			return nil, xerror.UnsupportedTransformRuntimeInput().Detail(
				"json runtime payload is empty",
			)
		}

		rows, columnNames, err := pipepayload.ParseJSONBodyToRows(upstreamPayload.Document)
		if err != nil {
			return nil, err
		}
		validationRows := rows
		if contract != nil {
			validationRows, _, err = pipepayload.ParseJSONBodyToRowsPreserveNumbers(upstreamPayload.Document)
			if err != nil {
				return nil, err
			}
		}
		if err := validateTransformInputContract(validationRows, contract, "runtime payload"); err != nil {
			return nil, err
		}

		input := &rowInput{
			Rows:        rows,
			ColumnNames: columnNames,
			RowCount:    len(rows),
		}
		if err := chargeInputRows(input.Rows, budget); err != nil {
			return nil, err
		}
		return input, nil
	case model.ContractRepresentationCSV:
		rows, columnNames, err := pipepayload.ParseCSVBodyToRows([]byte(upstreamPayload.CSV))
		if err != nil {
			return nil, err
		}
		validationRows := pipepayload.CoerceContractFieldValidationRows(contract, rows)
		if err := validateTransformInputContract(validationRows, contract, "runtime payload"); err != nil {
			return nil, err
		}

		input := &rowInput{
			Rows:        rows,
			ColumnNames: columnNames,
			RowCount:    len(rows),
		}
		if err := chargeInputRows(input.Rows, budget); err != nil {
			return nil, err
		}
		return input, nil
	default:
		return nil, xerror.UnsupportedTransformRuntimeInput()
	}
}

// validateTransformInputContract checks that all required fields declared in the input contract
// are present in the first row of the result. Field names support dot-notation paths
// (e.g. "profile.email", "items[].sku") resolved via payload field-path helpers.
// Validation is skipped when the contract is nil or the result is empty.
func validateTransformInputContract(
	rows []map[string]any,
	contract *pipepayload.Contract,
	source string,
) error {
	if contract == nil || len(rows) == 0 {
		return nil
	}

	row := rows[0]
	for _, field := range contract.Fields {
		if !field.Required {
			continue
		}
		_, exists, hasMissing, err := pipepayload.ResolveFieldPathValues(row, field.Name)
		if err != nil || !exists || hasMissing {
			return xerror.Wrapf(
				"required field %q not found in %s: %w",
				field.Name,
				source,
				xerror.Join(xerror.TransformInputContractViolation(), err),
			)
		}
	}
	if failure := pipepayload.ValidateContractFieldValues(contract, rows); failure != nil {
		return xerror.Wrapf(
			"%s: %w",
			failure.Summary(),
			xerror.Join(xerror.TransformInputContractViolation(), failure),
		)
	}
	return nil
}

func buildTransformExecutionResult(
	outputKind model.TransformOutputKind,
	rows []map[string]any,
	columnNames []string,
	budget *executionBudget,
) (*pipepayload.Output, error) {
	if err := budget.checkContext(); err != nil {
		return nil, err
	}
	if budget.limits.MaxOutputRows > 0 && len(rows) > budget.limits.MaxOutputRows {
		budget.outputRows = len(rows)
		return nil, ErrOutputRowsExceeded.Detailf("maximum output rows is %d", budget.limits.MaxOutputRows)
	}
	budget.outputRows = len(rows)
	transformOutput := &pipepayload.Output{
		OutputKind:  outputKind,
		RowCount:    len(rows),
		ColumnNames: columnNames,
	}

	switch outputKind {
	case model.TransformOutputKindUnknown, model.TransformOutputKindRows:
		outputSize, err := estimateTransformRowsBytes(rows, budget)
		if err != nil {
			return nil, err
		}
		if err := checkOutputBytes(outputSize, budget); err != nil {
			return nil, err
		}
		transformOutput.Rows = rowsWithRowOrientedJSONValues(rows)
		return transformOutput, nil
	case model.TransformOutputKindJSON:
		if _, err := estimateTransformRowsBytes(rows, budget); err != nil {
			return nil, err
		}
		document, err := json.Marshal(rows)
		if err != nil {
			return nil, xerror.Unexpected().Detailf("marshal transform JSON output: %w", err)
		}
		if err := checkOutputBytes(int64(len(document)), budget); err != nil {
			return nil, err
		}
		transformOutput.Document = document
		return transformOutput, nil
	case model.TransformOutputKindCSV:
		encodedCSV, err := encodeRowsAsCSV(rows, columnNames, budget)
		if err != nil {
			return nil, err
		}
		if err := checkOutputBytes(int64(len(encodedCSV)), budget); err != nil {
			return nil, err
		}
		transformOutput.CSV = encodedCSV
		return transformOutput, nil
	default:
		return nil, xerror.Wrapf(
			"output kind %q: %w",
			outputKind.String(),
			ErrRuntimeOutputKindUnsupported,
		)
	}
}

func encodeRowsAsCSV(
	rows []map[string]any,
	columnNames []string,
	budget *executionBudget,
) (string, error) {
	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)

	if err := writer.Write(columnNames); err != nil {
		return "", xerror.Unexpected().Detailf("encode csv header: %w", err)
	}

	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return "", err
		}
		record := make([]string, len(columnNames))
		for index, columnName := range columnNames {
			value, exists := row[columnName]
			if !exists || value == nil {
				record[index] = ""
				continue
			}

			record[index] = fmt.Sprint(value)
		}

		if err := writer.Write(record); err != nil {
			return "", xerror.Unexpected().Detailf("encode csv row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", xerror.Unexpected().Detailf("flush csv writer: %w", err)
	}

	return buffer.String(), nil
}

func applyOrderByAndLimit(
	selectStmt *ast.SelectStmt,
	rows []map[string]any,
	budget *executionBudget,
) ([]map[string]any, error) {
	if err := budget.checkContext(); err != nil {
		return nil, err
	}
	if selectStmt == nil {
		return rows, nil
	}

	finalRows := append([]map[string]any(nil), rows...)

	if selectStmt.OrderBy != nil && len(selectStmt.OrderBy.Items) > 0 {
		if err := applyOrderBy(selectStmt.OrderBy, finalRows, budget); err != nil {
			return nil, err
		}
	}

	if selectStmt.Limit != nil {
		limitedRows, err := applyLimit(selectStmt.Limit, finalRows)
		if err != nil {
			return nil, err
		}
		finalRows = limitedRows
	}

	return finalRows, nil
}

func applyOrderBy(
	orderBy *ast.OrderByClause,
	rows []map[string]any,
	budget *executionBudget,
) error {
	if orderBy == nil || len(orderBy.Items) == 0 {
		return nil
	}

	var sortErr error
	sort.SliceStable(rows, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		if err := budget.checkContext(); err != nil {
			sortErr = err
			return false
		}

		cmp, err := compareRowsByOrderBy(orderBy.Items, rows[i], rows[j], budget)
		if err != nil {
			sortErr = err
			return false
		}

		return cmp < 0
	})

	if sortErr != nil {
		return xerror.Wrapf("sort rows by ORDER BY clause: %w", sortErr)
	}

	return nil
}

func compareRowsByOrderBy(
	items []*ast.ByItem,
	leftRow, rightRow map[string]any,
	budget *executionBudget,
) (int, error) {
	for _, item := range items {
		if item == nil {
			continue
		}

		leftValue, err := evaluateTransformProjectionExpr(item.Expr, leftRow, budget, 1)
		if err != nil {
			return 0, xerror.Wrapf("evaluate left ORDER BY expression: %w", err)
		}
		rightValue, err := evaluateTransformProjectionExpr(item.Expr, rightRow, budget, 1)
		if err != nil {
			return 0, xerror.Wrapf("evaluate right ORDER BY expression: %w", err)
		}

		cmp, err := compareOrderByValues(leftValue, rightValue)
		if err != nil {
			return 0, xerror.Wrapf("compare ORDER BY values: %w", err)
		}

		if item.Desc {
			cmp = -cmp
		}

		if cmp != 0 {
			return cmp, nil
		}
	}

	return 0, nil
}

func compareOrderByValues(leftValue, rightValue any) (int, error) {
	if leftValue == nil && rightValue == nil {
		return 0, nil
	}
	if leftValue == nil {
		return 1, nil
	}
	if rightValue == nil {
		return -1, nil
	}

	comparison, isComparable, err := compareOrderedWhereValues(leftValue, rightValue)
	if err != nil {
		return 0, err
	}
	if !isComparable {
		return 0, xerror.Wrapf(
			"ORDER BY values are not comparable for %T and %T: %w",
			leftValue,
			rightValue,
			ErrRuntimeWhereExpressionUnsupported,
		)
	}

	return comparison, nil
}

func applyLimit(limit *ast.Limit, rows []map[string]any) ([]map[string]any, error) {
	if limit == nil {
		return rows, nil
	}

	offset, err := parseLimitValue(limit.Offset, 0)
	if err != nil {
		return nil, xerror.Wrapf("parse LIMIT offset: %w", err)
	}
	count, err := parseLimitValue(limit.Count, len(rows))
	if err != nil {
		return nil, xerror.Wrapf("parse LIMIT count: %w", err)
	}

	if offset < 0 || count < 0 {
		return nil, xerror.InvalidTransformLimitNegative()
	}

	if offset >= len(rows) {
		return []map[string]any{}, nil
	}

	end := offset + count
	if end > len(rows) {
		end = len(rows)
	}

	return rows[offset:end], nil
}

func parseLimitValue(expr ast.ExprNode, defaultValue int) (int, error) {
	if expr == nil {
		return defaultValue, nil
	}

	valueExpr, ok := expr.(ast.ValueExpr)
	if !ok {
		return 0, xerror.Unexpected().Detailf("unsupported LIMIT expression type %T", expr)
	}

	value := valueExpr.GetValue()
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		if v > uint(math.MaxInt) {
			return 0, xerror.Unexpected().Detailf("LIMIT value %d exceeds int range", v)
		}
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		if strconv.IntSize == 32 && v > math.MaxInt32 {
			return 0, xerror.Unexpected().Detailf("LIMIT value %d exceeds int range", v)
		}
		return int(v), nil
	case uint64:
		if v > uint64(math.MaxInt) {
			return 0, xerror.Unexpected().Detailf("LIMIT value %d exceeds int range", v)
		}
		return int(v), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, xerror.Unexpected().Detailf("parse LIMIT string %q: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, xerror.Unexpected().Detailf("unsupported LIMIT literal type %T", value)
	}
}

func evaluateTransformGroupedRows(
	selectStmt *ast.SelectStmt,
	rows []map[string]any,
	budget *executionBudget,
) ([]map[string]any, error) {
	groupBuckets, err := buildGroupBuckets(selectStmt, rows, budget)
	if err != nil {
		return nil, err
	}

	projectedRows := make([]map[string]any, 0, len(groupBuckets))
	for _, bucket := range groupBuckets {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		projected := make(map[string]any, len(selectStmt.Fields.Fields))
		for _, field := range selectStmt.Fields.Fields {
			if field == nil || field.Auxiliary {
				continue
			}

			name, err := projectionFieldName(field)
			if err != nil {
				return nil, err
			}

			value, err := evaluateTransformGroupedProjectionExpr(
				field.Expr,
				bucket.rows,
				bucket.representative,
				budget,
				1,
			)
			if err != nil {
				return nil, err
			}
			projected[name] = value
		}
		projectedSize, err := estimateTransformRowBytes(projected, budget, 1)
		if err != nil {
			return nil, err
		}
		if err := budget.chargeIntermediate(1, projectedSize); err != nil {
			return nil, err
		}

		projectedRows = append(projectedRows, projected)
	}

	return projectedRows, nil
}

func isTransformAggregateProjectionExpr(expr ast.ExprNode) bool {
	switch node := expr.(type) {
	case *ast.AggregateFuncExpr:
		return node != nil
	case *ast.FuncCastExpr:
		return node != nil && isTransformAggregateProjectionExpr(node.Expr)
	default:
		return false
	}
}

func evaluateTransformGroupedProjectionExpr(
	expr ast.ExprNode,
	rows []map[string]any,
	representative map[string]any,
	budget *executionBudget,
	depth int,
) (any, error) {
	if castExpr, ok := expr.(*ast.FuncCastExpr); ok && castExpr != nil &&
		isTransformAggregateProjectionExpr(castExpr.Expr) {
		if err := budget.checkExpression(depth); err != nil {
			return nil, err
		}
		value, err := evaluateTransformGroupedProjectionExpr(
			castExpr.Expr,
			rows,
			representative,
			budget,
			depth+1,
		)
		if err != nil {
			return nil, err
		}
		return castRuntimeValue(value, castExpr)
	}

	if _, ok := expr.(*ast.AggregateFuncExpr); ok {
		return evaluateTransformAggregateExpr(expr, rows, budget, depth)
	}

	return evaluateTransformProjectionExpr(expr, representative, budget, depth)
}

type runtimeGroupBucket struct {
	representative map[string]any
	rows           []map[string]any
}

func buildGroupBuckets(
	selectStmt *ast.SelectStmt,
	rows []map[string]any,
	budget *executionBudget,
) ([]runtimeGroupBucket, error) {
	if selectStmt == nil || selectStmt.GroupBy == nil {
		return []runtimeGroupBucket{{rows: rows, representative: firstOrEmptyRow(rows)}}, nil
	}

	if len(rows) == 0 {
		return []runtimeGroupBucket{}, nil
	}

	buckets := make([]runtimeGroupBucket, 0)
	indexByKey := make(map[string]int)
	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		groupKey, err := groupKeyForRow(selectStmt.GroupBy, row)
		if err != nil {
			return nil, err
		}

		idx, exists := indexByKey[groupKey]
		if !exists {
			idx = len(buckets)
			indexByKey[groupKey] = idx
			buckets = append(
				buckets,
				runtimeGroupBucket{representative: row, rows: []map[string]any{row}},
			)
			continue
		}

		buckets[idx].rows = append(buckets[idx].rows, row)
	}

	return buckets, nil
}

func groupKeyForRow(groupBy *ast.GroupByClause, row map[string]any) (string, error) {
	if groupBy == nil {
		return "", nil
	}

	parts := make([]string, 0, len(groupBy.Items))
	for _, item := range groupBy.Items {
		if item == nil {
			continue
		}

		columnExpr, ok := item.Expr.(*ast.ColumnNameExpr)
		if !ok || columnExpr == nil || columnExpr.Name == nil {
			return "", xerror.Unexpected().Detail(
				"group by supports only column references at runtime",
			)
		}

		fieldName := transformColumnReferenceName(columnExpr.Name)
		value, exists, err := getRowFieldValueForReference(row, fieldName)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", xerror.Wrapf(
				"field %q not found in transform input row: %w",
				fieldName,
				ErrRuntimeProjectionFieldNotFound,
			)
		}

		parts = append(parts, fmt.Sprintf("%T:%v", value, value))
	}

	return strings.Join(parts, "|"), nil
}

func firstOrEmptyRow(rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return map[string]any{}
	}

	return rows[0]
}

func applyWhereFilter(
	selectStmt *ast.SelectStmt,
	rows []map[string]any,
	budget *executionBudget,
) ([]map[string]any, error) {
	if selectStmt == nil || selectStmt.Where == nil {
		return rows, nil
	}

	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return nil, err
		}
		keep, err := evaluateWhereExpression(selectStmt.Where, row, budget, 1)
		if err != nil {
			return nil, xerror.Wrapf("evaluate WHERE expression: %w", err)
		}
		if keep {
			filtered = append(filtered, row)
		}
	}

	return filtered, nil
}

func evaluateWhereExpression(
	expr ast.ExprNode,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (bool, error) {
	if err := budget.checkExpression(depth); err != nil {
		return false, err
	}
	if expr == nil {
		return true, nil
	}

	if parenthesizedExpr, ok := expr.(*ast.ParenthesesExpr); ok && parenthesizedExpr != nil {
		return evaluateWhereExpression(parenthesizedExpr.Expr, row, budget, depth+1)
	}

	if unaryExpr, ok := expr.(*ast.UnaryOperationExpr); ok && unaryExpr != nil {
		switch unaryExpr.Op {
		case opcode.Not:
			value, err := evaluateWhereExpression(unaryExpr.V, row, budget, depth+1)
			if err != nil {
				return false, err
			}
			return !value, nil
		default:
			return false, xerror.Wrapf(
				"unsupported unary WHERE operator %q: %w",
				unaryExpr.Op.String(),
				ErrRuntimeWhereExpressionUnsupported,
			)
		}
	}

	if binaryExpr, ok := expr.(*ast.BinaryOperationExpr); ok && binaryExpr != nil {
		switch binaryExpr.Op {
		case opcode.LogicAnd:
			left, err := evaluateWhereExpression(binaryExpr.L, row, budget, depth+1)
			if err != nil {
				return false, err
			}
			if !left {
				return false, nil
			}
			return evaluateWhereExpression(binaryExpr.R, row, budget, depth+1)
		case opcode.LogicOr:
			left, err := evaluateWhereExpression(binaryExpr.L, row, budget, depth+1)
			if err != nil {
				return false, err
			}
			if left {
				return true, nil
			}
			return evaluateWhereExpression(binaryExpr.R, row, budget, depth+1)
		case opcode.EQ, opcode.NE, opcode.LT, opcode.LE, opcode.GT, opcode.GE:
			return evaluateWhereComparison(binaryExpr, row, budget, depth)
		default:
			return false, xerror.Wrapf(
				"unsupported binary WHERE operator %q: %w",
				binaryExpr.Op.String(),
				ErrRuntimeWhereExpressionUnsupported,
			)
		}
	}

	if isNullExpr, ok := expr.(*ast.IsNullExpr); ok && isNullExpr != nil {
		value, err := evaluateTransformProjectionExpr(isNullExpr.Expr, row, budget, depth+1)
		if err != nil {
			return false, err
		}
		isNull := value == nil
		if isNullExpr.Not {
			return !isNull, nil
		}
		return isNull, nil
	}

	value, err := evaluateTransformProjectionExpr(expr, row, budget, depth+1)
	if err != nil {
		return false, err
	}

	booleanValue, err := toBoolRuntimeValue(value)
	if err != nil {
		return false, xerror.Wrapf("coerce WHERE expression to boolean: %w", err)
	}

	return booleanValue, nil
}

func evaluateWhereComparison(
	expr *ast.BinaryOperationExpr,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (bool, error) {
	leftValue, err := evaluateTransformProjectionExpr(expr.L, row, budget, depth+1)
	if err != nil {
		return false, err
	}
	rightValue, err := evaluateTransformProjectionExpr(expr.R, row, budget, depth+1)
	if err != nil {
		return false, err
	}

	switch expr.Op {
	case opcode.EQ:
		return whereValuesEqual(leftValue, rightValue), nil
	case opcode.NE:
		return !whereValuesEqual(leftValue, rightValue), nil
	case opcode.LT, opcode.LE, opcode.GT, opcode.GE:
		comparison, isComparable, err := compareOrderedWhereValues(leftValue, rightValue)
		if err != nil {
			return false, err
		}
		if !isComparable {
			return false, nil
		}

		switch expr.Op {
		case opcode.LT:
			return comparison < 0, nil
		case opcode.LE:
			return comparison <= 0, nil
		case opcode.GT:
			return comparison > 0, nil
		case opcode.GE:
			return comparison >= 0, nil
		default:
			panic("unreachable: constrained by outer LT/LE/GT/GE case")
		}
	default:
		return false, xerror.Wrapf(
			"unsupported comparison operator %q: %w",
			expr.Op.String(),
			ErrRuntimeWhereExpressionUnsupported,
		)
	}
}

func whereValuesEqual(leftValue, rightValue any) bool {
	if leftValue == nil || rightValue == nil {
		return leftValue == rightValue
	}

	leftNumeric, leftErr := toWhereEqualityNumber(leftValue)
	rightNumeric, rightErr := toWhereEqualityNumber(rightValue)
	if leftErr == nil && rightErr == nil {
		return leftNumeric == rightNumeric
	}

	return fmt.Sprintf("%v", leftValue) == fmt.Sprintf("%v", rightValue)
}

func toWhereEqualityNumber(value any) (float64, error) {
	if booleanValue, ok := value.(bool); ok {
		if booleanValue {
			return 1, nil
		}
		return 0, nil
	}

	return toFloat64(value)
}

func compareOrderedWhereValues(leftValue, rightValue any) (int, bool, error) {
	if leftValue == nil || rightValue == nil {
		return 0, false, nil
	}

	leftNumeric, leftErr := toFloat64(leftValue)
	rightNumeric, rightErr := toFloat64(rightValue)
	if leftErr == nil && rightErr == nil {
		switch {
		case leftNumeric < rightNumeric:
			return -1, true, nil
		case leftNumeric > rightNumeric:
			return 1, true, nil
		default:
			return 0, true, nil
		}
	}

	leftString, leftStringOK := leftValue.(string)
	rightString, rightStringOK := rightValue.(string)
	if leftStringOK && rightStringOK {
		switch {
		case leftString < rightString:
			return -1, true, nil
		case leftString > rightString:
			return 1, true, nil
		default:
			return 0, true, nil
		}
	}

	return 0, false, xerror.Wrapf(
		"ordered WHERE comparison is unsupported for %T and %T: %w",
		leftValue,
		rightValue,
		ErrRuntimeWhereExpressionUnsupported,
	)
}

func toBoolRuntimeValue(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case nil:
		return false, nil
	case int:
		return v != 0, nil
	case int8:
		return v != 0, nil
	case int16:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case uint:
		return v != 0, nil
	case uint8:
		return v != 0, nil
	case uint16:
		return v != 0, nil
	case uint32:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case float32:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(v))
		if trimmed == "true" || trimmed == "1" {
			return true, nil
		}
		if trimmed == "false" || trimmed == "0" || trimmed == "" {
			return false, nil
		}
		return false, xerror.Unexpected().Detailf("unsupported string boolean value %q", v)
	default:
		return false, xerror.Unexpected().Detailf("unsupported boolean type %T", value)
	}
}

func evaluateTransformProjectionExpr(
	expr ast.ExprNode,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (any, error) {
	if err := budget.checkExpression(depth); err != nil {
		return nil, err
	}
	if castExpr, ok := expr.(*ast.FuncCastExpr); ok && castExpr != nil {
		value, err := evaluateTransformProjectionExpr(castExpr.Expr, row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		return castRuntimeValue(value, castExpr)
	}

	if fnExpr, ok := expr.(*ast.FuncCallExpr); ok && fnExpr != nil {
		return evaluateTransformFuncCallExpr(fnExpr, row, budget, depth)
	}

	if valueExpr, ok := expr.(ast.ValueExpr); ok {
		return valueExpr.GetValue(), nil
	}

	if binaryExpr, ok := expr.(*ast.BinaryOperationExpr); ok && binaryExpr != nil {
		return evaluateBinaryProjectionExpr(binaryExpr, row, budget, depth)
	}

	columnExpr, ok := expr.(*ast.ColumnNameExpr)
	if !ok || columnExpr == nil || columnExpr.Name == nil {
		return nil, xerror.Wrapf(
			"unsupported transform projection expression at runtime: %w",
			ErrRuntimeProjectionExpressionUnsupported,
		)
	}

	fieldName := transformColumnReferenceName(columnExpr.Name)
	value, exists, err := getRowFieldValueForReference(row, fieldName)
	if err != nil {
		return nil, err
	}
	if exists {
		return value, nil
	}
	return nil, xerror.Wrapf(
		"field %q not found in transform input row: %w",
		fieldName,
		ErrRuntimeProjectionFieldNotFound,
	)
}

func transformColumnReferenceName(columnName *ast.ColumnName) string {
	if columnName == nil {
		return ""
	}
	if columnName.Table.L != "" {
		return columnName.Table.L + "." + columnName.Name.L
	}
	return columnName.Name.L
}

func evaluateTransformFuncCallExpr(
	fnExpr *ast.FuncCallExpr,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (any, error) {
	if fnExpr == nil {
		return nil, xerror.Wrapf(
			"unsupported transform projection expression at runtime: %w",
			ErrRuntimeProjectionExpressionUnsupported,
		)
	}

	switch strings.ToLower(fnExpr.FnName.L) {
	case jsonFuncObject:
		if len(fnExpr.Args)%2 != 0 {
			return nil, xerror.Wrapf(
				"json_object expects key/value argument pairs: %w",
				ErrRuntimeProjectionExpressionUnsupported,
			)
		}

		value := make(map[string]any, len(fnExpr.Args)/2)
		for i := 0; i < len(fnExpr.Args); i += 2 {
			keyValue, err := evaluateTransformProjectionExpr(fnExpr.Args[i], row, budget, depth+1)
			if err != nil {
				return nil, err
			}

			key, ok := keyValue.(string)
			if !ok {
				return nil, xerror.Wrapf(
					"json_object key must be a string literal: %w",
					ErrRuntimeProjectionExpressionUnsupported,
				)
			}

			argValue, err := evaluateTransformProjectionExpr(fnExpr.Args[i+1], row, budget, depth+1)
			if err != nil {
				return nil, err
			}
			value[key] = argValue
		}

		return newTransformJSONConstructedValue(value), nil

	case jsonFuncArray:
		value := make([]any, 0, len(fnExpr.Args))
		for _, arg := range fnExpr.Args {
			argValue, err := evaluateTransformProjectionExpr(arg, row, budget, depth+1)
			if err != nil {
				return nil, err
			}
			value = append(value, argValue)
		}

		return newTransformJSONConstructedValue(value), nil

	case "json_extract":
		if len(fnExpr.Args) != 2 {
			return nil, xerror.Wrapf(
				"json_extract expects two arguments: %w",
				ErrRuntimeProjectionExpressionUnsupported,
			)
		}

		docValue, err := evaluateTransformProjectionExpr(fnExpr.Args[0], row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		pathValue, err := evaluateTransformProjectionExpr(fnExpr.Args[1], row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		path, ok := pathValue.(string)
		if !ok {
			return nil, xerror.Wrapf(
				"json_extract path must be a string literal: %w",
				ErrRuntimeProjectionExpressionUnsupported,
			)
		}

		if err := validateHeadlessCSVPathAccess(docValue, path); err != nil {
			return nil, err
		}

		return extractJSONPathValue(docValue, path)
	}

	return nil, xerror.Wrapf(
		"unsupported transform projection expression at runtime: %w",
		ErrRuntimeProjectionExpressionUnsupported,
	)
}

func validateHeadlessCSVPathAccess(docValue any, path string) error {
	normalized, err := normalizeJSONValue(docValue)
	if err != nil {
		return err
	}

	// Named access on array-backed values indicates a CSV-headless style misuse.
	if _, isArray := normalized.([]any); !isArray {
		return nil
	}

	tokens, err := tokenizeJSONPath(path)
	if err != nil {
		return err
	}

	for _, token := range tokens {
		if token.kind == jsonPathTokenKey {
			return xerror.Wrapf(
				"path %q uses named access for a headless CSV/array value; use numeric index selectors like $[0]: %w",
				path,
				ErrRuntimeHeadlessCSVNamedAccessUnsupported,
			)
		}
	}

	return nil
}

func extractJSONPathValue(docValue any, path string) (any, error) {
	normalized, err := normalizeJSONValue(docValue)
	if err != nil {
		return nil, err
	}

	tokens, err := tokenizeJSONPath(path)
	if err != nil {
		return nil, err
	}

	current := normalized
	for _, token := range tokens {
		switch token.kind {
		case jsonPathTokenKey:
			obj, ok := current.(map[string]any)
			if !ok {
				return nil, xerror.Unexpected().Detailf(
					"json path %q expected object at key %q",
					path,
					token.key,
				)
			}
			value, exists := obj[token.key]
			if !exists {
				return nil, xerror.Unexpected().Detailf(
					"json path %q key %q not found",
					path,
					token.key,
				)
			}
			current = value
		case jsonPathTokenIndex:
			arr, ok := current.([]any)
			if !ok {
				return nil, xerror.Unexpected().Detailf(
					"json path %q expected array at index %d",
					path,
					token.index,
				)
			}
			if token.index < 0 || token.index >= len(arr) {
				return nil, xerror.Unexpected().Detailf(
					"json path %q index %d out of bounds",
					path,
					token.index,
				)
			}
			current = arr[token.index]
		}
	}

	return current, nil
}

func normalizeJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case transformJSONConstructedValue:
		return nativeTransformJSONValue(v), nil
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			return nil, xerror.Unexpected().Detailf("parse json value: %w", err)
		}
		return decoded, nil
	case []byte:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			return nil, xerror.Unexpected().Detailf("parse json value: %w", err)
		}
		return decoded, nil
	default:
		return value, nil
	}
}

type jsonPathTokenKind int

const (
	jsonPathTokenKey jsonPathTokenKind = iota
	jsonPathTokenIndex
)

type jsonPathToken struct {
	kind  jsonPathTokenKind
	key   string
	index int
}

func tokenizeJSONPath(path string) ([]jsonPathToken, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "$" {
		return nil, nil
	}
	if !strings.HasPrefix(trimmed, "$") {
		return nil, xerror.Unexpected().Detail("json path must start with '$'")
	}

	cursor := 1
	tokens := make([]jsonPathToken, 0)
	for cursor < len(trimmed) {
		switch trimmed[cursor] {
		case '.':
			cursor++
			start := cursor
			for cursor < len(trimmed) {
				ch := trimmed[cursor]
				if ch == '.' || ch == '[' {
					break
				}
				cursor++
			}
			if start == cursor {
				return nil, xerror.Unexpected().Detail("json path contains empty object key")
			}
			tokens = append(
				tokens,
				jsonPathToken{kind: jsonPathTokenKey, key: trimmed[start:cursor]},
			)
		case '[':
			cursor++
			start := cursor
			for cursor < len(trimmed) && trimmed[cursor] != ']' {
				cursor++
			}
			if cursor >= len(trimmed) {
				return nil, xerror.Unexpected().Detail(
					"json path contains unterminated index selector",
				)
			}
			indexLiteral := strings.TrimSpace(trimmed[start:cursor])
			indexValue, err := strconv.Atoi(indexLiteral)
			if err != nil {
				return nil, xerror.Unexpected().Detailf(
					"json path index %q is invalid",
					indexLiteral,
				)
			}
			tokens = append(tokens, jsonPathToken{kind: jsonPathTokenIndex, index: indexValue})
			cursor++
		default:
			return nil, xerror.Unexpected().Detailf(
				"json path has unsupported token %q",
				string(trimmed[cursor]),
			)
		}
	}

	return tokens, nil
}

func evaluateBinaryProjectionExpr(
	expr *ast.BinaryOperationExpr,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (any, error) {
	switch expr.Op {
	case opcode.Plus:
		leftValue, err := evaluateTransformProjectionExpr(expr.L, row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		rightValue, err := evaluateTransformProjectionExpr(expr.R, row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		leftNumeric, err := toFloat64(leftValue)
		if err != nil {
			return nil, xerror.Wrapf("left operand numeric conversion failed: %w", err)
		}
		rightNumeric, err := toFloat64(rightValue)
		if err != nil {
			return nil, xerror.Wrapf("right operand numeric conversion failed: %w", err)
		}
		return leftNumeric + rightNumeric, nil
	case opcode.Minus:
		leftValue, err := evaluateTransformProjectionExpr(expr.L, row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		rightValue, err := evaluateTransformProjectionExpr(expr.R, row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		leftNumeric, err := toFloat64(leftValue)
		if err != nil {
			return nil, xerror.Wrapf("left operand numeric conversion failed: %w", err)
		}
		rightNumeric, err := toFloat64(rightValue)
		if err != nil {
			return nil, xerror.Wrapf("right operand numeric conversion failed: %w", err)
		}
		return leftNumeric - rightNumeric, nil
	case opcode.Mul:
		leftValue, err := evaluateTransformProjectionExpr(expr.L, row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		rightValue, err := evaluateTransformProjectionExpr(expr.R, row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		leftNumeric, err := toFloat64(leftValue)
		if err != nil {
			return nil, xerror.Wrapf("left operand numeric conversion failed: %w", err)
		}
		rightNumeric, err := toFloat64(rightValue)
		if err != nil {
			return nil, xerror.Wrapf("right operand numeric conversion failed: %w", err)
		}
		return leftNumeric * rightNumeric, nil
	case opcode.Div:
		leftValue, err := evaluateTransformProjectionExpr(expr.L, row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		rightValue, err := evaluateTransformProjectionExpr(expr.R, row, budget, depth+1)
		if err != nil {
			return nil, err
		}

		leftNumeric, err := toFloat64(leftValue)
		if err != nil {
			return nil, xerror.Wrapf("left operand numeric conversion failed: %w", err)
		}
		rightNumeric, err := toFloat64(rightValue)
		if err != nil {
			return nil, xerror.Wrapf("right operand numeric conversion failed: %w", err)
		}
		if rightNumeric == 0 {
			return nil, xerror.InvalidTransformDivisionByZero()
		}
		return leftNumeric / rightNumeric, nil
	default:
		return nil, xerror.Wrapf(
			"unsupported transform projection binary operator %q at runtime: %w",
			expr.Op.String(),
			ErrRuntimeProjectionExpressionUnsupported,
		)
	}
}

func evaluateTransformAggregateExpr(
	expr ast.ExprNode,
	rows []map[string]any,
	budget *executionBudget,
	depth int,
) (any, error) {
	if err := budget.checkExpression(depth); err != nil {
		return nil, err
	}
	aggExpr, ok := expr.(*ast.AggregateFuncExpr)
	if !ok || aggExpr == nil {
		return nil, xerror.Wrapf(
			"unsupported transform aggregate expression at runtime: %w",
			ErrRuntimeAggregateExpressionUnsupported,
		)
	}

	switch strings.ToLower(aggExpr.F) {
	case ast.AggFuncCount, ast.AggFuncApproxCountDistinct:
		if len(aggExpr.Args) == 0 {
			return int64(len(rows)), nil
		}
		if len(aggExpr.Args) == 1 && isCountStarArg(aggExpr.Args[0]) {
			return int64(len(rows)), nil
		}

		columnExpr, ok := aggExpr.Args[0].(*ast.ColumnNameExpr)
		if !ok || columnExpr == nil || columnExpr.Name == nil {
			return nil, aggregateColumnReferenceRuntimeError("count")
		}

		fieldName := transformColumnReferenceName(columnExpr.Name)
		if aggExpr.Distinct {
			seen := make(map[string]struct{}, len(rows))
			for _, row := range rows {
				if err := budget.checkContext(); err != nil {
					return nil, err
				}
				value, exists, err := getRowFieldValueForReference(row, fieldName)
				if err != nil {
					return nil, err
				}
				if !exists || value == nil {
					continue
				}
				seen[fmt.Sprintf("%T:%v", value, value)] = struct{}{}
			}
			return int64(len(seen)), nil
		}

		nonNullCount := int64(0)
		for _, row := range rows {
			if err := budget.checkContext(); err != nil {
				return nil, err
			}
			value, exists, err := getRowFieldValueForReference(row, fieldName)
			if err != nil {
				return nil, err
			}
			if !exists || value == nil {
				continue
			}
			nonNullCount++
		}

		return nonNullCount, nil
	case ast.AggFuncSum:
		if len(aggExpr.Args) == 0 {
			return nil, xerror.InvalidTransformSumArgumentRequired()
		}

		fieldName := aggregateArgFieldName(aggExpr.Args[0])
		total := float64(0)
		hasValue := false
		for _, row := range rows {
			numeric, exists, err := evaluateNumericAggregateArg(aggExpr.Args[0], row, budget, depth+1)
			if err != nil {
				return nil, xerror.Wrapf("sum field %q: %w", fieldName, err)
			}
			if !exists {
				continue
			}
			hasValue = true
			total += numeric
		}
		if !hasValue {
			return nil, nil //nolint:nilnil
		}
		return total, nil
	case ast.AggFuncAvg:
		if len(aggExpr.Args) == 0 {
			return nil, xerror.InvalidTransformAvgArgumentRequired()
		}

		fieldName := aggregateArgFieldName(aggExpr.Args[0])
		total := float64(0)
		count := float64(0)
		for _, row := range rows {
			numeric, exists, err := evaluateNumericAggregateArg(aggExpr.Args[0], row, budget, depth+1)
			if err != nil {
				return nil, xerror.Wrapf("avg field %q: %w", fieldName, err)
			}
			if !exists {
				continue
			}
			total += numeric
			count++
		}
		if count == 0 {
			return nil, nil //nolint:nilnil
		}
		return total / count, nil
	case ast.AggFuncMax, ast.AggFuncMin:
		if len(aggExpr.Args) == 0 {
			return nil, xerror.Unexpected().Detailf(
				"%s aggregate requires an argument",
				strings.ToLower(aggExpr.F),
			)
		}

		fieldName := aggregateArgFieldName(aggExpr.Args[0])
		var selected float64
		hasValue := false
		for _, row := range rows {
			numeric, exists, err := evaluateNumericAggregateArg(aggExpr.Args[0], row, budget, depth+1)
			if err != nil {
				return nil, xerror.Wrapf(
					"%s field %q: %w",
					strings.ToLower(aggExpr.F),
					fieldName,
					err,
				)
			}
			if !exists {
				continue
			}

			if !hasValue {
				selected = numeric
				hasValue = true
				continue
			}

			if strings.EqualFold(aggExpr.F, ast.AggFuncMax) && numeric > selected {
				selected = numeric
			}
			if strings.EqualFold(aggExpr.F, ast.AggFuncMin) && numeric < selected {
				selected = numeric
			}
		}

		if !hasValue {
			return nil, nil //nolint:nilnil
		}

		return selected, nil
	default:
		return nil, xerror.Unexpected().Detailf(
			"unsupported transform aggregate function %q at runtime",
			aggExpr.F,
		)
	}
}

func aggregateArgFieldName(expr ast.ExprNode) string {
	if columnExpr, ok := expr.(*ast.ColumnNameExpr); ok && columnExpr != nil &&
		columnExpr.Name != nil {
		return transformColumnReferenceName(columnExpr.Name)
	}

	if castExpr, ok := expr.(*ast.FuncCastExpr); ok && castExpr != nil {
		if columnExpr, ok := castExpr.Expr.(*ast.ColumnNameExpr); ok && columnExpr != nil &&
			columnExpr.Name != nil {
			return transformColumnReferenceName(columnExpr.Name)
		}
	}

	return "<expr>"
}

func evaluateNumericAggregateArg(
	expr ast.ExprNode,
	row map[string]any,
	budget *executionBudget,
	depth int,
) (float64, bool, error) {
	if err := budget.checkExpression(depth); err != nil {
		return 0, false, err
	}
	if expr == nil {
		return 0, false, aggregateColumnReferenceRuntimeError("aggregate")
	}

	columnExpr, ok := expr.(*ast.ColumnNameExpr)
	if ok && columnExpr != nil && columnExpr.Name != nil {
		value, exists, err := getRowFieldValueForReference(
			row,
			transformColumnReferenceName(columnExpr.Name),
		)
		if err != nil {
			return 0, false, err
		}
		if !exists || value == nil {
			return 0, false, nil
		}

		numeric, err := toFloat64(value)
		if err != nil {
			return 0, false, err
		}

		return numeric, true, nil
	}

	castExpr, ok := expr.(*ast.FuncCastExpr)
	if !ok || castExpr == nil {
		return 0, false, aggregateColumnReferenceRuntimeError("aggregate")
	}

	innerColumn, ok := castExpr.Expr.(*ast.ColumnNameExpr)
	if !ok || innerColumn == nil || innerColumn.Name == nil {
		return 0, false, aggregateColumnReferenceRuntimeError("aggregate")
	}

	value, exists, err := getRowFieldValueForReference(
		row,
		transformColumnReferenceName(innerColumn.Name),
	)
	if err != nil {
		return 0, false, err
	}
	if !exists || value == nil {
		return 0, false, nil
	}

	castValue, err := castRuntimeValue(value, castExpr)
	if err != nil {
		return 0, false, err
	}

	numeric, err := toFloat64(castValue)
	if err != nil {
		return 0, false, err
	}

	return numeric, true, nil
}

func isCountStarArg(expr ast.ExprNode) bool {
	if expr == nil {
		return false
	}

	if strings.TrimSpace(expr.Text()) == "*" {
		return true
	}

	switch v := expr.(type) {
	case *ast.ColumnNameExpr:
		if v.Name == nil {
			return false
		}
		return v.Name.Name.L == "*" || v.Name.Name.O == "*"
	case ast.ValueExpr:
		literal := strings.TrimSpace(fmt.Sprint(v.GetValue()))
		if literal == "*" {
			return true
		}
		// TiDB parser commonly normalizes COUNT(*) to COUNT(1).
		if literal == "1" {
			return true
		}
		return false
	default:
		return false
	}
}

type ambiguousTransformFieldValue struct {
	fieldName string
}

func getRowFieldValueForReference(row map[string]any, fieldName string) (any, bool, error) {
	value, exists := getRowFieldValue(row, fieldName)
	if !exists {
		return nil, false, nil
	}

	if ambiguous, ok := value.(ambiguousTransformFieldValue); ok {
		ambiguousFieldName := ambiguous.fieldName
		if ambiguousFieldName == "" {
			ambiguousFieldName = fieldName
		}
		return nil, false, xerror.Wrapf(
			"field %q is ambiguous in transform input row: %w",
			ambiguousFieldName,
			ErrRuntimeProjectionFieldNotFound,
		)
	}

	return value, true, nil
}

func getRowFieldValue(row map[string]any, fieldName string) (any, bool) {
	key, exists := rowFieldKey(row, fieldName)
	if !exists {
		return nil, false
	}
	return row[key], true
}

func rowFieldKey(row map[string]any, fieldName string) (string, bool) {
	if _, exists := row[fieldName]; exists {
		return fieldName, true
	}

	for candidateKey := range row {
		if strings.EqualFold(candidateKey, fieldName) {
			return candidateKey, true
		}
	}

	return "", false
}

func aggregateColumnReferenceRuntimeError(functionName string) error {
	return xerror.Wrapf(
		"%s aggregate supports only column references at runtime: %w",
		functionName,
		ErrRuntimeAggregateColumnReference,
	)
}

func toFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, xerror.Unexpected().Detailf("parse numeric string %q: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, xerror.Unexpected().Detailf("unsupported numeric type %T", value)
	}
}

func castRuntimeValue(value any, castExpr *ast.FuncCastExpr) (any, error) {
	if castExpr == nil || castExpr.Tp == nil {
		return nil, xerror.InvalidTransformCastTargetRequired()
	}
	if value == nil {
		return nil, nil //nolint:nilnil
	}

	targetType := strings.ToLower(castExpr.Tp.String())

	if strings.Contains(targetType, "char") || strings.Contains(targetType, "text") ||
		strings.Contains(targetType, "string") {
		return fmt.Sprintf("%v", value), nil
	}

	if strings.Contains(targetType, "int") {
		switch v := value.(type) {
		case int:
			return int64(v), nil
		case int8:
			return int64(v), nil
		case int16:
			return int64(v), nil
		case int32:
			return int64(v), nil
		case int64:
			return v, nil
		case uint:
			if v > uint(math.MaxInt64) {
				return nil, xerror.Unexpected().Detailf("integer value %d exceeds int64 range", v)
			}
			return int64(v), nil
		case uint8:
			return int64(v), nil
		case uint16:
			return int64(v), nil
		case uint32:
			return int64(v), nil
		case uint64:
			if v > math.MaxInt64 {
				return nil, xerror.Unexpected().Detailf("integer value %d exceeds int64 range", v)
			}
			return int64(v), nil
		case float32:
			return int64(v), nil
		case float64:
			return int64(v), nil
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil, xerror.Unexpected().Detailf("cast value %q to integer: %w", v, err)
			}
			return parsed, nil
		default:
			return nil, xerror.Unexpected().Detailf(
				"unsupported cast source type %T to integer",
				value,
			)
		}
	}

	if strings.Contains(targetType, "numeric") ||
		strings.Contains(targetType, "decimal") ||
		strings.Contains(targetType, "float") ||
		strings.Contains(targetType, "double") ||
		strings.Contains(targetType, "real") {
		numeric, err := toFloat64(value)
		if err != nil {
			return nil, xerror.Wrapf("cast value %v to number: %w", value, err)
		}
		return numeric, nil
	}

	return nil, xerror.Unexpected().Detailf(
		"unsupported cast target type %q at runtime",
		castExpr.Tp.String(),
	)
}
