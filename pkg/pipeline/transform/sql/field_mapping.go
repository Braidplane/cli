package sql

import (
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/structvalidate"
	"github.com/Braidplane/cli/pkg/xerror"
)

var ErrFieldMappingUnsupported = xerror.New(xerror.KindInvalidInput,
	"PIPELINE_FIELD_MAPPING_UNSUPPORTED", "SQL cannot be edited as a field mapping")

// FieldMapping is an editing view of ordinary SQL, never a persisted transform language.
type FieldMapping struct {
	Input  string         `json:"input"`
	Fields []MappingField `json:"fields"`
}

type MappingField struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

// ReadFieldMapping refuses SQL whose behavior cannot survive projection editing.
// Callers must retain the original SQL when this returns an error.
func ReadFieldMapping(query string) (*FieldMapping, error) {
	// Comments are authored content too; the AST cannot round-trip them.
	if mappingSQLHasComments(query) {
		return nil, ErrFieldMappingUnsupported.Detail("SQL containing comments remains available in advanced editing")
	}
	stmt, _, err := parseTransformSelect(query)
	if err != nil {
		return nil, err
	}
	if len(stmt.unionAll) != 0 || stmt.From == nil || stmt.From.TableRefs == nil || stmt.From.TableRefs.Right != nil {
		return nil, ErrFieldMappingUnsupported.Detail("use advanced SQL for multiple relations or set operations")
	}
	source, ok := stmt.From.TableRefs.Left.(*ast.TableSource)
	if !ok {
		return nil, ErrFieldMappingUnsupported
	}
	table, ok := source.Source.(*ast.TableName)
	if !ok || table.Schema.O != "" || source.AsName.O != "" {
		return nil, ErrFieldMappingUnsupported.Detail("use advanced SQL for subqueries and relation aliases")
	}
	mapping := &FieldMapping{Input: table.Name.O, Fields: []MappingField{}}
	seen := make(map[string]bool)
	for _, field := range stmt.Fields.Fields {
		if field.WildCard != nil || field.Expr == nil {
			return nil, ErrFieldMappingUnsupported.Detail("select explicit fields before using guided mapping")
		}
		visitor := &mappingExpressionVisitor{}
		field.Expr.Accept(visitor)
		if visitor.unsupported {
			return nil, ErrFieldMappingUnsupported.Detail("use advanced SQL for aggregate or window expressions")
		}
		name, err := projectionFieldName(field)
		if err != nil || !mappingFieldName(name) || seen[name] {
			return nil, ErrFieldMappingUnsupported.Detail("use unique flat output names and name every expression")
		}
		seen[name] = true
		expression, err := restoreMappingSQL(field.Expr)
		if err != nil {
			return nil, err
		}
		mapping.Fields = append(mapping.Fields, MappingField{Name: name, Expression: expression})
	}
	projection, err := parseMappingProjection(*mapping)
	if err != nil {
		return nil, err
	}
	// Comparing complete restored statements fails closed on every clause the
	// editing view does not carry, including future parser additions.
	clone := *stmt.SelectStmt
	clone.Fields = projection.Fields
	original, err := restoreMappingSQL(&clone)
	if err != nil {
		return nil, err
	}
	canonical, err := restoreMappingSQL(projection.SelectStmt)
	if err != nil {
		return nil, err
	}
	if original != canonical {
		return nil, ErrFieldMappingUnsupported.Detail("use advanced SQL for filters, ordering, grouping, CTEs and other query clauses")
	}
	return mapping, nil
}

// WriteFieldMapping emits SQL validated by the existing transform parser.
func WriteFieldMapping(mapping FieldMapping) (string, error) {
	stmt, err := parseMappingProjection(mapping)
	if err != nil {
		return "", err
	}
	query, err := restoreMappingSQL(stmt.SelectStmt)
	if err != nil {
		return "", err
	}
	parsed, err := ReadFieldMapping(query)
	if err != nil {
		return "", err
	}
	if len(parsed.Fields) != len(mapping.Fields) || parsed.Input != mapping.Input {
		return "", ErrFieldMappingUnsupported.Detail("each mapping expression must produce exactly one field")
	}
	for i := range parsed.Fields {
		if parsed.Fields[i].Name != mapping.Fields[i].Name {
			return "", ErrFieldMappingUnsupported.Detail("mapping expressions cannot declare additional projections")
		}
	}
	return query, nil
}

// SuggestFieldMapping proposes exact same-name, compatible-type fields only.
// Requiredness, nullability and business meaning still require author review.
func SuggestFieldMapping(input string, source, destination *pipepayload.Contract) FieldMapping {
	mapping := FieldMapping{Input: input, Fields: []MappingField{}}
	if source == nil || destination == nil {
		return mapping
	}
	for _, target := range destination.Fields {
		if !mappingFieldName(target.Name) {
			continue
		}
		for _, field := range source.Fields {
			if field.Name != target.Name || !isCompatibleContractFieldType(field.Type, target.Type) {
				continue
			}
			if target.Type == model.ContractFieldTypeValueDecimal && !pipepayload.DecimalFieldCompatible(field, target) {
				continue
			}
			mapping.Fields = append(mapping.Fields, MappingField{Name: target.Name, Expression: quoteMappingIdentifier(field.Name)})
			break
		}
	}
	return mapping
}

func ValidateFieldMappingSchema(contract *pipepayload.Contract) error {
	if contract == nil || len(contract.Fields) == 0 {
		return ErrFieldMappingUnsupported.Detail("supply a nonempty resolved contract shape; an unavailable schema is not a passing check")
	}
	if err := structvalidate.Default().Struct(contract); err != nil {
		return ErrFieldMappingUnsupported.Detail("supply a named contract with valid typed fields")
	}
	seen := make(map[string]bool)
	for _, field := range contract.Fields {
		if seen[field.Name] {
			return ErrFieldMappingUnsupported.Detail("contract field names must be unique")
		}
		seen[field.Name] = true
	}
	return pipepayload.ValidateContractFieldDeclarations(contract)
}

// ValidateFieldMappingContracts checks explicit shapes, without resolving catalog
// references or claiming that cached metadata proves runtime values.
func ValidateFieldMappingContracts(query string, outputKind model.TransformOutputKind, source, destination *pipepayload.Contract) error {
	for _, contract := range []*pipepayload.Contract{source, destination} {
		if err := ValidateFieldMappingSchema(contract); err != nil {
			return err
		}
	}
	if _, err := ReadFieldMapping(query); err != nil {
		return err
	}
	if err := ValidateTransformOutputFacets(outputKind, destination); err != nil {
		return err
	}
	types := make(map[string]model.ContractFieldType, len(source.Fields))
	for _, field := range source.Fields {
		types[field.Name] = field.Type
	}
	return ValidateOutputContractTypesWithInputSchemaForOutputKind(query, outputKind, destination, model.DataSourceTypeUnknown, types)
}

// ValidateTransformOutputFacets checks the wire format separately from field types.
func ValidateTransformOutputFacets(kind model.TransformOutputKind, contract *pipepayload.Contract) error {
	if kind != model.TransformOutputKindUnknown && kind != model.TransformOutputKindRows && kind != model.TransformOutputKindJSON && kind != model.TransformOutputKindCSV {
		return ErrFieldMappingUnsupported.Detail("select rows, json or csv output")
	}
	if contract == nil {
		return nil
	}
	representation, root := outputKindContractFacets(kind)
	if (contract.Representation != model.ContractRepresentationUnknown && contract.Representation != representation) ||
		(contract.RootShape != model.ContractRootShapeUnknown && contract.RootShape != root) {
		return ErrOutputContractViolation.Detail("output representation or root shape differs from its contract")
	}
	return nil
}

func parseMappingProjection(mapping FieldMapping) (*transformSelect, error) {
	if err := ValidateTransformInputRelationName(mapping.Input); err != nil {
		return nil, err
	}
	if len(mapping.Fields) == 0 {
		return nil, ErrFieldMappingUnsupported.Detail("select at least one output field")
	}
	projections := make([]string, 0, len(mapping.Fields))
	for _, field := range mapping.Fields {
		if !mappingFieldName(field.Name) || strings.TrimSpace(field.Expression) == "" {
			return nil, ErrFieldMappingUnsupported.Detail("each field needs a lowercase flat output name and a SQL expression")
		}
		if mappingSQLHasComments(field.Expression) {
			return nil, ErrFieldMappingUnsupported.Detail("keep expression comments in advanced SQL editing")
		}
		projections = append(projections, field.Expression+" AS "+quoteMappingIdentifier(field.Name))
	}
	stmt, _, err := parseTransformSelect("SELECT " + strings.Join(projections, ", ") + " FROM " + quoteMappingIdentifier(mapping.Input))
	return stmt, err
}

func mappingFieldName(name string) bool {
	return strings.TrimSpace(name) != "" && name == strings.ToLower(name) && !strings.ContainsAny(name, ".[]\x00")
}

func quoteMappingIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func mappingSQLHasComments(query string) bool {
	var quote byte
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if quote != 0 {
			if ch == '\\' && quote != '`' {
				i++
			} else if ch == quote {
				if i+1 < len(query) && query[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '#':
			return true
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				return true
			}
		case '-':
			if i+2 < len(query) && query[i+1] == '-' && query[i+2] <= ' ' {
				return true
			}
		}
	}
	return false
}

func restoreMappingSQL(node ast.Node) (string, error) {
	var output strings.Builder
	if err := node.Restore(format.NewRestoreCtx(format.DefaultRestoreFlags|format.RestoreStringEscapeBackslash, &output)); err != nil {
		return "", xerror.Unexpected().With(err)
	}
	return output.String(), nil
}

type mappingExpressionVisitor struct{ unsupported bool }

func (v *mappingExpressionVisitor) Enter(node ast.Node) (ast.Node, bool) {
	switch node.(type) {
	case *ast.AggregateFuncExpr, *ast.WindowFuncExpr, *ast.SubqueryExpr:
		v.unsupported = true
	}
	return node, v.unsupported
}

func (v *mappingExpressionVisitor) Leave(node ast.Node) (ast.Node, bool) { return node, !v.unsupported }
