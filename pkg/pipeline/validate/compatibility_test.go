package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestEvaluateEdgeCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		producer   *payload.Contract
		consumer   *payload.Contract
		wantOK     bool
		wantReason ContractReason
	}{
		{
			name: "EDGE-001_QueryRowsToTransformRowsSubset",
			producer: schemaContract(
				"query-output",
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
				},
				payload.ContractField{
					Name:     "email",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			consumer: schemaContract(
				"transform-input",
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
				},
			),
			wantOK: true,
		},
		{
			name: "EDGE-002_QueryRowsToHTTPJSONSink",
			producer: schemaContract(
				"query-output",
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
				},
			),
			consumer: schemaContract(
				"http-sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
				},
			),
			wantReason: ContractReasonRepresentationMismatch,
		},
		{
			name: "EDGE-003_TransformJSONObjectToSinkJSONObject",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "payload",
					Type:     model.ContractFieldTypeValueObject,
					Required: true,
				},
			),
			consumer: schemaContract(
				"sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "payload",
					Type:     model.ContractFieldTypeValueObject,
					Required: true,
				},
			),
			wantOK: true,
		},
		{
			name: "EDGE-004_TransformRowsToHubspotJSONSink",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			consumer: schemaContract(
				"hubspot-sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			wantReason: ContractReasonRepresentationMismatch,
		},
		{
			name: "EDGE-005_TransformJSONStringToSinkJSONInteger",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{Name: "id", Type: model.ContractFieldTypeValueString, Required: true},
			),
			consumer: schemaContract(
				"sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
				},
			),
			wantReason: ContractReasonTypeMismatch,
		},
		{
			name: "EDGE-006_NullableProducerFieldRejectedByNonNullableConsumer",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: false,
					Nullable: true,
				},
			),
			consumer: schemaContract(
				"hubspot-sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
					Nullable: false,
				},
			),
			wantReason: ContractReasonNullabilityMismatch,
		},
		{
			name: "EDGE-007_TransformCSVToCSVSink",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationCSV,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			consumer: schemaContract(
				"csv-sink-input",
				model.ContractRepresentationCSV,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "email_address",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			wantOK: true,
		},
		{
			name: "EDGE-008_JSONArrayRejectedByJSONObjectSink",
			producer: schemaContract(
				"transform-output",
				model.ContractRepresentationJSON,
				model.ContractRootShapeArray,
				payload.ContractField{
					Name:     "payload",
					Type:     model.ContractFieldTypeValueObject,
					Required: true,
				},
			),
			consumer: schemaContract(
				"sink-input",
				model.ContractRepresentationJSON,
				model.ContractRootShapeObject,
				payload.ContractField{
					Name:     "payload",
					Type:     model.ContractFieldTypeValueObject,
					Required: true,
				},
			),
			wantReason: ContractReasonRootShapeMismatch,
		},
		{
			name: "EDGE-009_FileSourceToFileSink",
			producer: schemaContract(
				"file-output",
				model.ContractRepresentationFile,
				model.ContractRootShapeUnknown,
			),
			consumer: schemaContract(
				"file-sink-input",
				model.ContractRepresentationFile,
				model.ContractRootShapeUnknown,
			),
			wantOK: true,
		},
		{
			name: "EDGE-010_FileSourceRejectedByRowsSink",
			producer: schemaContract(
				"file-output",
				model.ContractRepresentationFile,
				model.ContractRootShapeUnknown,
			),
			consumer: schemaContract(
				"rows-sink-input",
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
				payload.ContractField{
					Name:     "id",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				},
			),
			wantReason: ContractReasonRepresentationMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EvaluateEdgeCompatibility(
				EdgeLink{
					FromStep: "fetch-users",
					ToStep:   "users-sync",
				},
				tc.producer,
				tc.consumer,
			)

			require.Equal(t, tc.wantOK, got.Compatible)
			require.Equal(t, tc.wantReason, got.ReasonCode)
			if tc.wantOK {
				require.Empty(t, got.Expected)
				require.Empty(t, got.Actual)
				return
			}

			require.NotEmpty(t, got.Expected)
			require.NotEmpty(t, got.Actual)
		})
	}
}

func TestValidateCompatibilityContract_CSVRootShapeRejected(t *testing.T) {
	t.Parallel()

	err := validateCompatibilityContract(&payload.Contract{
		Representation: model.ContractRepresentationCSV,
		RootShape:      model.ContractRootShapeObject,
		Fields: []payload.ContractField{{
			Name:     "id",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
		}},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "root_shape requires json representation")
}

func schemaContract(
	name string,
	representation model.ContractRepresentation,
	rootShape model.ContractRootShape,
	fields ...payload.ContractField,
) *payload.Contract {
	return &payload.Contract{
		Name:           name,
		Representation: representation,
		RootShape:      rootShape,
		Fields:         fields,
	}
}
