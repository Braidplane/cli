package sql

import (
	"encoding/json"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/opcode"
)

// indexTransformJoin indexes only the first ON conjunct, preserving the
// evaluator's short-circuit order; all retained candidates still evaluate ON.
func indexTransformJoin(on *ast.OnCondition, left, right []map[string]any, budget *executionBudget) (map[string][]map[string]any, []string, error) {
	if on == nil || len(left) < 2 || len(right) < 2 || int64(len(left))*int64(len(right))/2 <= int64(len(left))+int64(len(right)) {
		return nil, nil, nil
	}
	expr := on.Expr
	residual := false
	depth := 1
	for {
		if err := budget.checkExpression(depth); err != nil {
			return nil, nil, err
		}
		switch node := expr.(type) {
		case *ast.ParenthesesExpr:
			expr = node.Expr
		case *ast.BinaryOperationExpr:
			if node.Op != opcode.LogicAnd {
				goto equality
			}
			residual = true
			expr = node.L
		default:
			return nil, nil, nil
		}
		depth++
	}
equality:
	eq, ok := expr.(*ast.BinaryOperationExpr)
	if !ok || eq.Op != opcode.EQ {
		return nil, nil, nil
	}
	a, aOK := eq.L.(*ast.ColumnNameExpr)
	b, bOK := eq.R.(*ast.ColumnNameExpr)
	if !aOK || !bOK || a.Name.Table.L == "" || b.Name.Table.L == "" {
		return nil, nil, nil
	}
	if err := budget.checkExpression(depth + 1); err != nil {
		return nil, nil, err
	}
	leftField := a.Name.Table.L + "." + a.Name.Name.L
	rightField := b.Name.Table.L + "." + b.Name.Name.L
	if _, exists := rowFieldKey(left[0], leftField); !exists {
		leftField, rightField = rightField, leftField
	}
	if err := budget.chargeIntermediate(0, 48); err != nil {
		return nil, nil, err
	}
	keys := make([][]string, 2)
	for side, rows := range [][]map[string]any{left, right} {
		field, other := leftField, rightField
		if side == 1 {
			field, other = rightField, leftField
		}
		if err := budget.chargeIntermediate(0, int64(len(rows))*16); err != nil {
			return nil, nil, err
		}
		keys[side] = make([]string, len(rows))
		for i, row := range rows {
			if err := budget.chargeWork(1); err != nil {
				return nil, nil, err
			}
			name, exists := rowFieldKey(row, field)
			_, overlaps := rowFieldKey(row, other)
			if !exists || overlaps {
				return nil, nil, nil
			}
			value := row[name]
			// UNKNOWN AND a residual must still evaluate the residual.
			if value == nil && residual {
				return nil, nil, nil
			}
			key, supported := transformJoinKey(value)
			if !supported {
				return nil, nil, nil
			}
			if err := budget.chargeIntermediate(0, int64(len(key))); err != nil {
				return nil, nil, err
			}
			keys[side][i] = key
		}
	}
	if err := budget.chargeIntermediate(0, 64); err != nil {
		return nil, nil, err
	}
	index := make(map[string][]map[string]any)
	for i, key := range keys[1] {
		if err := budget.chargeWork(1); err != nil {
			return nil, nil, err
		}
		if key == "" {
			continue
		}
		size := int64(16)
		if _, exists := index[key]; !exists {
			size += 64
		}
		if err := budget.chargeIntermediate(0, size); err != nil {
			return nil, nil, err
		}
		index[key] = append(index[key], right[i])
	}
	return index, keys[0], nil
}

// Boolean/opaque/decimal keys retain the pair evaluator: mixed-type equality
// can fall back to formatting or reject decimal operands, so it is not a key.
func transformJoinKey(value any) (string, bool) {
	if value == nil {
		return "", true
	}
	switch value.(type) {
	case string, json.Number, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		number, err := numericValue(value)
		if err == nil {
			return "n:" + number.RatString(), true
		}
		if text, ok := value.(string); ok && !numericCandidate(text) {
			return "s:" + text, true
		}
	}
	return "", false
}
