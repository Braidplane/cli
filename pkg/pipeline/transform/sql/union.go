package sql

import (
	"slices"
	"time"

	"github.com/pingcap/tidb/pkg/parser/ast"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
)

// A union shares the SELECT pipeline, inference and execution budgets with its
// branches. The embedded header owns only WITH and final ORDER BY/LIMIT.
type transformSelect struct {
	*ast.SelectStmt
	unionAll    []*transformSelect
	typeDetails map[string]transformProjectionTypeDetail
}

func transformSelectStatement(node ast.Node) (*transformSelect, error) {
	switch stmt := node.(type) {
	case *ast.SelectStmt:
		return &transformSelect{SelectStmt: stmt}, nil
	case *ast.SetOprStmt:
		if stmt.SelectList == nil || len(stmt.SelectList.Selects) < 2 || stmt.SelectList.With != nil || stmt.SelectList.OrderBy != nil || stmt.SelectList.Limit != nil {
			return nil, ErrFeatureNotAllowed.Detail("UNION ALL requires two or more SELECT branches")
		}
		query := &transformSelect{SelectStmt: &ast.SelectStmt{With: stmt.With, OrderBy: stmt.OrderBy, Limit: stmt.Limit}}
		var names []string
		for i, node := range stmt.SelectList.Selects {
			branch, ok := node.(*ast.SelectStmt)
			if !ok || (i > 0 && (branch.AfterSetOperator == nil || *branch.AfterSetOperator != ast.UnionAll)) {
				return nil, ErrFeatureNotAllowed.Detail("only flat UNION ALL set operations are supported")
			}
			if branch.With != nil || branch.OrderBy != nil || branch.Limit != nil {
				return nil, ErrFeatureNotAllowed.Detail("UNION ALL branches cannot have WITH, ORDER BY or LIMIT; use a preceding CTE")
			}
			if branch.From == nil || branch.From.TableRefs == nil || branch.From.TableRefs.Right != nil || branch.GroupBy != nil || branch.Having != nil {
				return nil, ErrFeatureNotAllowed.Detail("UNION ALL branches must project one named row relation; normalize or aggregate in a preceding CTE or transform")
			}
			source, ok := branch.From.TableRefs.Left.(*ast.TableSource)
			if !ok {
				return nil, ErrFeatureNotAllowed.Detail("UNION ALL requires named input relations")
			}
			if _, ok := source.Source.(*ast.TableName); !ok {
				return nil, ErrFeatureNotAllowed.Detail("UNION ALL subqueries are not supported")
			}
			wrapped := &transformSelect{SelectStmt: branch}
			if _, err := extractProjectionFieldNames(wrapped); err != nil {
				return nil, err
			}
			fields := make([]string, 0, len(branch.Fields.Fields))
			for _, field := range branch.Fields.Fields {
				if _, ok := field.Expr.(*ast.ColumnNameExpr); !ok {
					return nil, ErrFeatureNotAllowed.Detail("UNION ALL branches support only column projections; normalize in a preceding CTE or transform")
				}
				name, err := projectionFieldName(field)
				if err != nil {
					return nil, err
				}
				fields = append(fields, name)
			}
			if i == 0 {
				names = fields
				query.Fields = branch.Fields
			} else if !slices.Equal(names, fields) {
				return nil, ErrContractType.Detailf("UNION ALL input %q projection fields %v differ from %v; normalize names and order upstream", unionInputName(wrapped), fields, names)
			}
			query.unionAll = append(query.unionAll, wrapped)
		}
		return query, nil
	default:
		return nil, ErrNotSelect
	}
}

// ValidateTransformInputContracts compares known structural declarations across
// UNION ALL inputs. Per-input quality declarations do not become global claims.
func ValidateTransformInputContracts(sql string, contracts map[string]*pipepayload.Contract) error {
	stmt, _, err := parseTransformSelect(sql)
	if err != nil {
		return err
	}
	_, err = transformKnownFields(stmt, contracts)
	return err
}

func transformKnownFields(stmt *transformSelect, contracts map[string]*pipepayload.Contract) (*pipepayload.Contract, error) {
	relations := make(map[string]*pipepayload.Contract, len(contracts))
	for name, contract := range contracts {
		relations[name] = contract
	}
	if stmt.With != nil {
		for _, cte := range stmt.With.CTEs {
			body, err := cteSelectStatement(cte)
			if err != nil {
				return nil, err
			}
			contract, err := transformKnownFields(body, relations)
			if err != nil {
				return nil, err
			}
			relations[cteRelationName(cte)] = contract
		}
	}
	if len(stmt.unionAll) > 0 {
		var canonical *pipepayload.Contract
		known := make(map[string]pipepayload.ContractField)
		for _, branch := range stmt.unionAll {
			contract, err := transformKnownFields(branch, relations)
			if err != nil {
				return nil, err
			}
			if canonical == nil {
				canonical = contract
			}
			if contract == nil {
				continue
			}
			for _, field := range contract.Fields {
				if expected, exists := known[field.Name]; exists && (field.Type != expected.Type || field.Nullable != expected.Nullable || field.Required != expected.Required || !equalOptionalInt(field.Precision, expected.Precision) || !equalOptionalInt(field.Scale, expected.Scale)) {
					return nil, ErrContractType.Detailf("UNION ALL input %q field %q has incompatible type, precision/scale, presence or nullability; normalize upstream", unionInputName(branch), field.Name)
				}
				known[field.Name] = field
			}

		}
		return canonical, nil
	}
	if stmt.From != nil && stmt.From.TableRefs != nil && stmt.From.TableRefs.Right != nil {
		return &pipepayload.Contract{}, nil
	}
	types := make(transformInputFieldTypes)
	for name, contract := range relations {
		if contract != nil {
			for _, field := range contract.Fields {
				types[name+"."+field.Name] = field.Type
			}
		}
	}
	aliases := types.relationAliases(stmt)
	for _, field := range stmt.Fields.Fields {
		if window, ok := field.Expr.(*ast.WindowFuncExpr); ok {
			items := append([]*ast.ByItem(nil), window.Spec.OrderBy.Items...)
			if window.Spec.PartitionBy != nil {
				items = append(items, window.Spec.PartitionBy.Items...)
			}
			for _, item := range items {
				typ := inferTransformRowFieldType(item.Expr, types, aliases)
				if typ != model.ContractFieldTypeUnknown && !supportedWindowKeyType(typ) {
					return nil, ErrContractType.Detailf("ROW_NUMBER window key type %s is not supported", typ)
				}
			}
		}
	}
	output := &pipepayload.Contract{}
	for _, projected := range stmt.Fields.Fields {
		name, err := projectionFieldName(projected)
		if err != nil {
			return nil, err
		}
		if col, ok := projected.Expr.(*ast.ColumnNameExpr); ok {
			source, ok := stmt.From.TableRefs.Left.(*ast.TableSource)
			if !ok {
				continue
			}
			table, ok := source.Source.(*ast.TableName)
			if !ok {
				continue
			}
			if col.Name.Table.L != "" && col.Name.Table.L != table.Name.L && col.Name.Table.L != source.AsName.L {
				continue
			}
			if contract := relations[table.Name.L]; contract != nil {
				for _, field := range contract.Fields {
					if field.Name == col.Name.Name.L {
						field.Name, field.Validations = name, nil
						output.Fields = append(output.Fields, field)
					}
				}
			}
			continue
		}
		fieldType := inferTransformRowFieldType(projected.Expr, types, aliases)
		if fieldType != model.ContractFieldTypeUnknown {
			precision, scale := decimalProjectionDeclaration(projected.Expr)
			output.Fields = append(output.Fields, pipepayload.ContractField{Name: name, Type: fieldType, Nullable: true, Required: true, Precision: precision, Scale: scale})
		}
	}
	return output, nil
}

func unionInputName(branch *transformSelect) string {
	return branch.From.TableRefs.Left.(*ast.TableSource).Source.(*ast.TableName).Name.L
}

func inferUnionAllProjection(stmt *transformSelect, source model.DataSourceType, types transformInputFieldTypes) (map[string]transformProjectionTypeDetail, error) {
	var result map[string]transformProjectionTypeDetail
	for i, branch := range stmt.unionAll {
		branch.typeDetails = stmt.typeDetails
		fields, err := extractTransformProjectionWithTypeDetails(branch, source, types, types.relationAliases(branch))
		if err != nil {
			return nil, err
		}
		if i == 0 {
			result = fields
			continue
		}
		for name, field := range fields {
			canonical := result[name]
			if canonical.fieldType != field.fieldType || !equalOptionalInt(canonical.precision, field.precision) || !equalOptionalInt(canonical.scale, field.scale) || canonical.constructedJSON != field.constructedJSON {
				return nil, ErrContractType.Detailf("UNION ALL input %q field %q has incompatible type or decimal precision/scale; normalize upstream", unionInputName(branch), name)
			}
			canonical.nonNull = canonical.nonNull && field.nonNull
			result[name] = canonical
		}
	}
	return result, nil
}

func equalOptionalInt(left, right *int) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func evaluateUnionAll(stmt *transformSelect, inputs map[string]*rowInput, budget *executionBudget, depth int) (*rowInput, error) {
	combined := &rowInput{}
	fieldTypes := make(map[string]model.ContractFieldType)
	for _, branch := range stmt.unionAll {
		rows, err := evaluateTransformSelectRows(branch, inputs, budget, depth+1)
		if err != nil {
			return nil, err
		}
		if combined.ColumnNames == nil {
			combined.ColumnNames = rows.ColumnNames
		}
		for _, row := range rows.Rows {
			if err := budget.chargeWork(1); err != nil {
				return nil, err
			}
			for _, name := range rows.ColumnNames {
				if err := budget.chargeWork(1); err != nil {
					return nil, err
				}
				value := row[name]
				if value == nil {
					continue
				}
				typ := pipepayload.InferFieldType(value)
				if _, ok := value.(time.Time); ok {
					typ = model.ContractFieldTypeValueDatetime
				}
				if typ == model.ContractFieldTypeUnknown {
					return nil, ErrContractType.Detailf("UNION ALL input %q field %q has unsupported runtime type %T; normalize upstream", unionInputName(branch), name, value)
				}
				if previous, known := fieldTypes[name]; known && previous != typ {
					return nil, ErrContractType.Detailf("UNION ALL input %q field %q has incompatible runtime type %s; expected %s", unionInputName(branch), name, typ, previous)
				}
				fieldTypes[name] = typ
			}
			size, err := estimateTransformRowBytes(row, budget, 1)
			if err != nil {
				return nil, err
			}
			if err := budget.chargeIntermediate(1, size); err != nil {
				return nil, err
			}
			combined.Rows = append(combined.Rows, row)
		}
	}
	rows, err := applyOrderByAndLimit(stmt, combined.Rows, budget)
	if err != nil {
		return nil, err
	}
	combined.Rows, combined.RowCount = rows, len(rows)
	return combined, nil
}
