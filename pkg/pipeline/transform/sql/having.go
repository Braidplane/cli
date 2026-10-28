package sql

import (
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
)

// HAVING resolves unqualified projection aliases before grouped input columns.
func validateHavingSubset(stmt *transformSelect) error {
	if stmt.Having == nil && stmt.GroupBy == nil {
		hasAggregate := false
		for _, field := range stmt.Fields.Fields {
			hasAggregate = hasAggregate || isTransformAggregateProjectionExpr(field.Expr)
		}
		if !hasAggregate {
			return nil
		}
	}
	grouped := make(map[string]bool)
	if stmt.GroupBy != nil {
		for _, item := range stmt.GroupBy.Items {
			if col, ok := item.Expr.(*ast.ColumnNameExpr); ok {
				grouped[transformColumnReferenceName(col.Name)] = true
			}
		}
	}
	aliases := make(map[string]bool)
	for _, field := range stmt.Fields.Fields {
		name, err := projectionFieldName(field)
		if err != nil {
			return err
		}
		aliases[name] = true
		v := &havingReferenceVisitor{grouped: grouped}
		field.Expr.Accept(v)
		if v.err != nil {
			return v.err
		}
	}
	if stmt.Having == nil {
		return nil
	}
	syntax := &havingSyntaxVisitor{}
	stmt.Having.Expr.Accept(syntax)
	if syntax.err != nil {
		return syntax.err
	}
	v := &havingReferenceVisitor{grouped: grouped, aliases: aliases}
	stmt.Having.Expr.Accept(v)
	return v.err
}

type havingSyntaxVisitor struct{ err error }

func (v *havingSyntaxVisitor) Enter(n ast.Node) (ast.Node, bool) {
	switch n.(type) {
	case ast.ValueExpr, *ast.ColumnNameExpr, *ast.AggregateFuncExpr:
		return n, true
	case *ast.BinaryOperationExpr, *ast.UnaryOperationExpr, *ast.ParenthesesExpr, *ast.IsNullExpr, *ast.FuncCastExpr, *ast.FuncCallExpr:
		return n, false
	default:
		v.err = ErrFeatureNotAllowed.Detail("unsupported HAVING expression")
		return n, true
	}
}

func (v *havingSyntaxVisitor) Leave(n ast.Node) (ast.Node, bool) { return n, v.err == nil }

type havingReferenceVisitor struct {
	grouped map[string]bool
	aliases map[string]bool
	err     error
}

func (v *havingReferenceVisitor) Enter(n ast.Node) (ast.Node, bool) {
	switch node := n.(type) {
	case *ast.AggregateFuncExpr:
		return n, true
	case *ast.ColumnNameExpr:
		name := transformColumnReferenceName(node.Name)
		if !havingGroupedColumn(name, v.grouped) && (node.Name.Table.L != "" || !v.aliases[name]) {
			v.err = ErrFeatureNotAllowed.Detailf("HAVING column %q must be grouped or a projection alias", name)
		}
		return n, true
	}
	return n, false
}

func havingGroupedColumn(name string, grouped map[string]bool) bool {
	for key := range grouped {
		if key == name {
			return true
		}
		if !strings.Contains(key, ".") && strings.HasSuffix(name, "."+key) {
			return true
		}
		if !strings.Contains(name, ".") && strings.HasSuffix(key, "."+name) {
			return true
		}
	}
	return false
}

func (v *havingReferenceVisitor) Leave(n ast.Node) (ast.Node, bool) {
	return n, v.err == nil
}

func evaluateHaving(expr ast.ExprNode, bucket runtimeGroupBucket, projected map[string]any, budget *executionBudget) (predicateValue, error) {
	v := &havingValueVisitor{bucket: bucket, projected: projected, budget: budget}
	materialized, ok := expr.Accept(v)
	if !ok || v.err != nil {
		return predicateUnknown, v.err
	}
	return evaluateWhereExpression(materialized.(ast.ExprNode), bucket.representative, budget, 1)
}

type havingValueVisitor struct {
	bucket    runtimeGroupBucket
	projected map[string]any
	budget    *executionBudget
	err       error
	depth     int
}

func (v *havingValueVisitor) Enter(n ast.Node) (ast.Node, bool) {
	v.depth++
	if v.err = v.budget.checkExpression(v.depth); v.err != nil {
		return n, true
	}
	if v.err = v.budget.chargeWork(1); v.err != nil {
		return n, true
	}
	// Copy expression containers: Accept rewrites children, and each group must
	// evaluate the original expression rather than the preceding group's values.
	switch node := n.(type) {
	case *ast.AggregateFuncExpr:
		var value any
		if v.err = v.budget.chargeWork(int64(len(v.bucket.rows))); v.err != nil {
			return n, true
		}
		value, v.err = evaluateTransformAggregateExpr(node, v.bucket.rows, v.budget, 1)
		return ast.NewValueExpr(value, "", ""), true
	case *ast.ColumnNameExpr:
		if value, ok := v.projected[node.Name.Name.L]; ok && node.Name.Table.L == "" {
			return ast.NewValueExpr(value, "", ""), true
		}
		return n, true
	case *ast.BinaryOperationExpr:
		cloned := *node
		return &cloned, false
	case *ast.UnaryOperationExpr:
		cloned := *node
		return &cloned, false
	case *ast.ParenthesesExpr:
		cloned := *node
		return &cloned, false
	case *ast.IsNullExpr:
		cloned := *node
		return &cloned, false
	case *ast.FuncCastExpr:
		cloned := *node
		return &cloned, false
	case *ast.FuncCallExpr:
		cloned := *node
		cloned.Args = append([]ast.ExprNode(nil), node.Args...)
		return &cloned, false
	case ast.ValueExpr:
		return n, true
	default:
		v.err = ErrFeatureNotAllowed.Detail("unsupported HAVING expression")
		return n, true
	}
}

func (v *havingValueVisitor) Leave(n ast.Node) (ast.Node, bool) {
	v.depth--
	return n, v.err == nil
}

type havingSchemaVisitor struct {
	types     transformInputFieldTypes
	aliases   transformInputRelationAliases
	projected map[string]bool
	err       error
}

func (v *havingSchemaVisitor) Enter(n ast.Node) (ast.Node, bool) {
	switch node := n.(type) {
	case *ast.AggregateFuncExpr:
		v.err = validateTransformInputExpressionReferences(node, v.types, v.aliases)
		return n, true
	case *ast.ColumnNameExpr:
		if node.Name.Table.L == "" && v.projected[node.Name.Name.L] {
			return n, true
		}
		v.err = validateTransformInputExpressionReferences(node, v.types, v.aliases)
		return n, true
	}
	return n, false
}

func (v *havingSchemaVisitor) Leave(n ast.Node) (ast.Node, bool) {
	return n, v.err == nil
}
