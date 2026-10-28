package sql

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pingcap/tidb/pkg/parser/ast"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
)

func validateRowNumber(window *ast.WindowFuncExpr) error {
	if !strings.EqualFold(window.Name, "row_number") || len(window.Args) != 0 || window.Distinct || window.IgnoreNull || window.FromLast || window.Spec.Frame != nil || window.Spec.Name.L != "" || window.Spec.Ref.L != "" || window.Spec.OnlyAlias {
		return ErrFeatureNotAllowed.Detail("window supports only ROW_NUMBER() with inline PARTITION BY/ORDER BY and no frame")
	}
	if window.Spec.OrderBy == nil || len(window.Spec.OrderBy.Items) == 0 {
		return ErrFeatureNotAllowed.Detail("ROW_NUMBER window requires explicit ORDER BY and a stable tie-breaker")
	}
	items := append([]*ast.ByItem(nil), window.Spec.OrderBy.Items...)
	if window.Spec.PartitionBy != nil {
		items = append(items, window.Spec.PartitionBy.Items...)
	}
	for _, item := range items {
		if _, ok := item.Expr.(*ast.ColumnNameExpr); !ok {
			return ErrFeatureNotAllowed.Detail("ROW_NUMBER partition and order keys must be input column references")
		}
	}
	return nil
}

// Window values are computed before projection so ORDER BY can use hidden input
// keys while subsequent transforms or CTEs can filter the projected row number.
func evaluateRowNumbers(stmt *transformSelect, rows []map[string]any, budget *executionBudget) (map[*ast.WindowFuncExpr][]int64, error) {
	result := make(map[*ast.WindowFuncExpr][]int64)
	for _, field := range stmt.Fields.Fields {
		window, ok := field.Expr.(*ast.WindowFuncExpr)
		if !ok {
			continue
		}
		if err := budget.chargeIntermediate(0, int64(len(rows))*16); err != nil {
			return nil, err
		}
		numbers := make([]int64, len(rows))
		partitions := make(map[string][]int)
		for i, row := range rows {
			if err := budget.chargeWork(1); err != nil {
				return nil, err
			}
			key := ""
			if window.Spec.PartitionBy != nil {
				for _, item := range window.Spec.PartitionBy.Items {
					value, err := evaluateTransformProjectionExpr(item.Expr, row, budget, 1)
					if err != nil {
						return nil, err
					}
					part, err := windowPartitionKey(value)
					if err != nil {
						return nil, err
					}
					key += fmt.Sprintf("%d:%s", len(part), part)
				}
			}
			// Validate singleton partition keys too; a sort need not compare them.
			for _, item := range window.Spec.OrderBy.Items {
				value, err := evaluateTransformProjectionExpr(item.Expr, row, budget, 1)
				if err != nil {
					return nil, err
				}
				if _, err := compareOrderByValues(value, value); err != nil {
					return nil, err
				}
			}
			if err := budget.chargeIntermediate(0, int64(len(key))+80); err != nil {
				return nil, err
			}
			partitions[key] = append(partitions[key], i)
		}
		for _, partition := range partitions {
			var sortErr error
			compare := func(left, right int) (int, error) {
				if err := budget.chargeWork(int64(len(window.Spec.OrderBy.Items))); err != nil {
					return 0, err
				}
				return compareRowsByOrderBy(window.Spec.OrderBy.Items, rows[left], rows[right], budget)
			}
			sort.Slice(partition, func(i, j int) bool {
				if sortErr != nil {
					return false
				}
				var cmp int
				cmp, sortErr = compare(partition[i], partition[j])
				return cmp < 0
			})
			if sortErr != nil {
				return nil, sortErr
			}
			for rank, index := range partition {
				if err := budget.checkContext(); err != nil {
					return nil, err
				}
				if rank > 0 {
					cmp, err := compare(partition[rank-1], index)
					if err != nil {
						return nil, err
					}
					if cmp == 0 {
						return nil, ErrFeatureNotAllowed.Detail("ROW_NUMBER ordering has tied keys; add a stable unique tie-breaker")
					}
				}
				numbers[index] = int64(rank + 1)
			}
		}
		result[window] = numbers
	}
	return result, nil
}

func windowPartitionKey(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case string:
		return "s:" + v, nil
	case bool:
		return "", ErrFeatureNotAllowed.Detail("ROW_NUMBER boolean partition keys are not supported")
	case time.Time:
		return "t:" + v.UTC().Format(time.RFC3339Nano), nil
	case decimal.Value:
		return "n:" + v.Rat().RatString(), nil
	default:
		if number, err := numericValue(value); err == nil {
			return "n:" + number.RatString(), nil
		}
		return "", ErrFeatureNotAllowed.Detailf("ROW_NUMBER partition key type %T is not supported", value)
	}
}

func supportedWindowKeyType(t model.ContractFieldType) bool {
	return t == model.ContractFieldTypeValueString || t == model.ContractFieldTypeValueInteger || t == model.ContractFieldTypeValueNumber || t == model.ContractFieldTypeValueDecimal || t == model.ContractFieldTypeValueDatetime
}

func validateWindowPlacement(stmt *transformSelect) error {
	v := &windowPlacementVisitor{allowed: make(map[*ast.WindowFuncExpr]bool)}
	for _, field := range stmt.Fields.Fields {
		if window, ok := field.Expr.(*ast.WindowFuncExpr); ok {
			v.allowed[window] = true
		}
	}
	if len(v.allowed) > 0 {
		for _, field := range stmt.Fields.Fields {
			if isTransformAggregateProjectionExpr(field.Expr) {
				return ErrFeatureNotAllowed.Detail("ROW_NUMBER must run in a separate transform or CTE after aggregation")
			}
		}
	}

	with := stmt.With
	stmt.With = nil
	defer func() { stmt.With = with }()
	stmt.Accept(v)
	return v.err
}

type windowPlacementVisitor struct {
	allowed map[*ast.WindowFuncExpr]bool
	err     error
}

func (v *windowPlacementVisitor) Enter(n ast.Node) (ast.Node, bool) {
	if window, ok := n.(*ast.WindowFuncExpr); ok && !v.allowed[window] {
		v.err = ErrFeatureNotAllowed.Detail("ROW_NUMBER window must be a direct SELECT projection; filter its alias in a following transform or CTE")
		return n, true
	}
	return n, false
}

func (v *windowPlacementVisitor) Leave(n ast.Node) (ast.Node, bool) { return n, v.err == nil }
