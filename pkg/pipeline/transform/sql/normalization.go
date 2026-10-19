package sql

import (
	"strings"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/opcode"
)

func calendarLayout(expr ast.ExprNode) (string, bool) {
	literal, ok := expr.(ast.ValueExpr)
	if !ok {
		return "", false
	}
	switch literal.GetValue() {
	case "%Y-%m-%d":
		return "2006-01-02", true
	case "%d.%m.%Y":
		return "02.01.2006", true
	default:
		return "", false
	}
}

func validateNormalizationFunction(fn *ast.FuncCallExpr) error {
	switch fn.FnName.L {
	case "coalesce":
		if len(fn.Args) >= 2 {
			return nil
		}
	case "nullif":
		if len(fn.Args) == 2 {
			return nil
		}
	case "trim", "lower", "upper":
		if len(fn.Args) == 1 {
			return nil
		}
	case "str_to_date", "date_format":
		if len(fn.Args) == 2 {
			if _, ok := calendarLayout(fn.Args[1]); ok {
				return nil
			}
		}
		return ErrFeatureNotAllowed.Detail("calendar date functions require a literal %Y-%m-%d or %d.%m.%Y format")
	case jsonFuncObject, jsonFuncArray, "json_extract":
		return nil
	default:
		return ErrFeatureNotAllowed.Detailf("function %s is not supported", fn.FnName.L)
	}
	return ErrFeatureNotAllowed.Detailf("invalid argument count for function %s", fn.FnName.L)
}

func validateCaseExpression(expr *ast.CaseExpr) error {
	if expr.Value != nil || len(expr.WhenClauses) == 0 {
		return ErrFeatureNotAllowed.Detail("only searched CASE WHEN predicates are supported")
	}
	for _, clause := range expr.WhenClauses {
		if err := validateCasePredicate(clause.Expr); err != nil {
			return err
		}
		visitor := &disallowedNodeVisitor{projection: true, scalarDepth: 1}
		clause.Result.Accept(visitor)
		if visitor.err != nil {
			return visitor.err
		}
	}
	if expr.ElseClause != nil {
		visitor := &disallowedNodeVisitor{projection: true, scalarDepth: 1}
		expr.ElseClause.Accept(visitor)
		return visitor.err
	}
	return nil
}

func validateCasePredicate(expr ast.ExprNode) error {
	switch node := expr.(type) {
	case *ast.ParenthesesExpr:
		return validateCasePredicate(node.Expr)
	case *ast.UnaryOperationExpr:
		if node.Op == opcode.Not {
			return validateCasePredicate(node.V)
		}
	case *ast.BinaryOperationExpr:
		switch node.Op {
		case opcode.LogicAnd, opcode.LogicOr:
			if err := validateCasePredicate(node.L); err != nil {
				return err
			}
			return validateCasePredicate(node.R)
		case opcode.EQ, opcode.NE, opcode.LT, opcode.LE, opcode.GT, opcode.GE:
			visitor := &disallowedNodeVisitor{projection: true, scalarDepth: 1}
			node.L.Accept(visitor)
			node.R.Accept(visitor)
			return visitor.err
		default:
			return ErrFeatureNotAllowed.Detail("unsupported CASE predicate operator")
		}
	case *ast.IsNullExpr:
		visitor := &disallowedNodeVisitor{projection: true, scalarDepth: 1}
		node.Expr.Accept(visitor)
		return visitor.err
	}
	return ErrFeatureNotAllowed.Detail("CASE WHEN requires comparisons, IS NULL, AND, OR or NOT")
}

func inferNormalizationType(args []ast.ExprNode, fields transformInputFieldTypes, aliases transformInputRelationAliases) model.ContractFieldType {
	result := model.ContractFieldTypeUnknown
	for _, arg := range args {
		if arg == nil || isNullLiteral(arg) {
			continue
		}
		inferred := inferTransformRowFieldType(arg, fields, aliases)
		if inferred == model.ContractFieldTypeUnknown {
			return inferred
		}
		if result == model.ContractFieldTypeUnknown {
			result = inferred
			continue
		}
		if inferred == result {
			continue
		}
		if (result == model.ContractFieldTypeValueInteger && inferred == model.ContractFieldTypeValueNumber) ||
			(result == model.ContractFieldTypeValueNumber && inferred == model.ContractFieldTypeValueInteger) {
			result = model.ContractFieldTypeValueNumber
		} else {
			return model.ContractFieldTypeUnknown
		}
	}
	return result
}

func evaluateNormalizationFunction(fn *ast.FuncCallExpr, row map[string]any, budget *executionBudget, depth int) (any, error) {
	if err := budget.chargeWork(1); err != nil {
		return nil, err
	}
	first, err := evaluateTransformProjectionExpr(fn.Args[0], row, budget, depth+1)
	if err != nil {
		return nil, err
	}
	switch fn.FnName.L {
	case "coalesce":
		for _, arg := range fn.Args[1:] {
			if first != nil {
				return first, nil
			}
			if err := budget.chargeWork(1); err != nil {
				return nil, err
			}
			first, err = evaluateTransformProjectionExpr(arg, row, budget, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return first, nil
	case "nullif":
		second, err := evaluateTransformProjectionExpr(fn.Args[1], row, budget, depth+1)
		if err != nil {
			return nil, err
		}
		if first == nil || second == nil {
			return first, nil
		}
		equal, err := whereValuesEqual(first, second)
		if err != nil {
			return nil, err
		}
		if equal {
			return nil, nil //nolint:nilnil // SQL NULLIF.
		}
		return first, nil
	}
	if first == nil {
		return nil, nil //nolint:nilnil // SQL NULL propagation.
	}
	value, ok := first.(string)
	if !ok {
		return nil, ErrRuntimeProjectionExpressionUnsupported.Detail("normalization requires a string input")
	}
	if err := budget.chargeWork(int64(len(value))); err != nil {
		return nil, err
	}
	switch fn.FnName.L {
	case "trim":
		return strings.TrimSpace(value), nil
	case "lower":
		return strings.ToLower(value), nil
	case "upper":
		return strings.ToUpper(value), nil
	case "str_to_date", "date_format":
		layout, _ := calendarLayout(fn.Args[1])
		inputLayout := layout
		if fn.FnName.L == "date_format" {
			inputLayout = "2006-01-02"
		}
		parsed, err := time.Parse(inputLayout, value)
		if err != nil || parsed.Year() < 1 || parsed.Format(inputLayout) != value {
			return nil, ErrRuntimeProjectionExpressionUnsupported.Detailf("%s: invalid calendar date for declared format", fn.FnName.L)
		}
		if fn.FnName.L == "str_to_date" {
			return parsed.Format("2006-01-02"), nil
		}
		return parsed.Format(layout), nil
	}
	return nil, ErrRuntimeProjectionExpressionUnsupported
}

func isNormalizationFunction(name string) bool {
	switch name {
	case "coalesce", "nullif", "trim", "lower", "upper", "str_to_date", "date_format":
		return true
	default:
		return false
	}
}
