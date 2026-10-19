package pipeline

import (
	"struct"
	"time"
)

// Pipeline schema for v0alpha2.
//
// Runtime validation enforces graph semantics that CUE cannot express cleanly:
// - query steps are source roots and cannot consume incoming graph edges
// - transform inputs and sink input must reference prior query or transform steps
// - query parameter references must point to prior query or transform producers
// - source/sink operations must set exactly one of operation_ref or operation_inline
// - multiple query roots are valid only when the graph converges downstream
#Pipeline: {
	metadata: {
		slug: #Slug
		name: string & !=""
	}
	steps: [...#Step] & [#Step, ...]
}

#Slug: string & =~"^[a-z0-9]+(?:-[a-z0-9]+)*$"

#Step: #InputStep | #QueryStep | #TransformStep | #SinkStep

#StepInput: #Slug

// Source data source operation types. SQL covers PostgreSQL and MySQL sources.
#QueryOperationType: "sql" | "bigquery" | "google_workspace" | "snowflake" | "http" | "hubspot" | "ftp" | "mongodb" | "git" | "s3"

// Sink operation types. HubSpot is intentionally source-only in the current pipeline contract.
#SinkOperationType: "sql" | "bigquery" | "google_workspace" | "snowflake" | "http" | "ftp" | "mongodb" | "git" | "s3"

#OperationRefTarget: {
	operation_ref:     #Slug
	operation_inline?: null
	...
}

#OperationInlineTarget: {
	operation_ref?: ""
	operation_inline: ({[string]: _}) & struct.MinFields(1)
	...
}

#ContractRepresentation: "rows" | "json" | "csv" | "file" | "multipart_form"
#ContractRootShape:      "object" | "array"
#ContractFieldType:      "string" | "integer" | "number" | "decimal" | "boolean" | "object" | "array" | "datetime"
#ContractFieldFormat:    "email" | "uuid" | "url"
#RFC3339DateTime:        string & time.Format(time.RFC3339)

#LengthBoundOrdering: {
	min_length?: int & >=0
	max_length?: int & >=0
	if min_length != _|_ && max_length != _|_ {
		min_length: <=max_length
	}
}

#DatetimeBoundOrdering: {
	min?: #RFC3339DateTime
	max?: #RFC3339DateTime
	if min != _|_ && max != _|_ {
		_min: time.Parse(time.RFC3339, min)
		_max: time.Parse(time.RFC3339, max)
		_min: <=_max
	}
}

#StringFieldValueValidations: {
	min_length?: int & >=0
	max_length?: int & >=0
	if min_length != _|_ && max_length != _|_ {
		min_length: <=max_length
	}
	format?: #ContractFieldFormat
	enum?: [...string] & [_, ...]
}

#IntegerFieldValueValidations: {
	enum?: [...int] & [_, ...]
	min?: int
	max?: int
	if min != _|_ && max != _|_ {
		min: <=max
	}
}

#NumberFieldValueValidations: {
	enum?: [...number] & [_, ...]
	min?: number
	max?: number
	if min != _|_ && max != _|_ {
		min: <=max
	}
}

#BooleanFieldValueValidations: {
	enum?: [...bool] & [_, ...]
}

#DatetimeFieldValueValidations: {
	min?: #RFC3339DateTime
	max?: #RFC3339DateTime
	if min != _|_ && max != _|_ {
		_min: time.Parse(time.RFC3339, min)
		_max: time.Parse(time.RFC3339, max)
		_min: <=_max
	}
}

#ArrayFieldValidations: {
	min_length?: int & >=0
	max_length?: int & >=0
	if min_length != _|_ && max_length != _|_ {
		min_length: <=max_length
	}
}

#StringFieldValidations: {
	min_length?: int & >=0
	max_length?: int & >=0
	if min_length != _|_ && max_length != _|_ {
		min_length: <=max_length
	}
	format?: #ContractFieldFormat
	enum?: [...string] & [_, ...]
	unique?: bool
}

#IntegerFieldValidations: {
	enum?: [...int] & [_, ...]
	min?:    int
	max?:    int
	unique?: bool
	if min != _|_ && max != _|_ {
		min: <=max
	}
}

#NumberFieldValidations: {
	enum?: [...number] & [_, ...]
	min?:    number
	max?:    number
	unique?: bool
	if min != _|_ && max != _|_ {
		min: <=max
	}
}

#BooleanFieldValidations: {
	enum?: [...bool] & [_, ...]
	unique?: bool
}

#DatetimeFieldValidations: {
	min?: #RFC3339DateTime
	max?: #RFC3339DateTime
	if min != _|_ && max != _|_ {
		_min: time.Parse(time.RFC3339, min)
		_max: time.Parse(time.RFC3339, max)
		_min: <=_max
	}
	unique?: bool
}

#FieldPath: string & =~"^(?:[^.\\[]+|\\[[^\\]]+\\])(?:\\[\\])?(?:\\.(?:[^.\\[]+|\\[[^\\]]+\\])(?:\\[\\])?)*$"

#SchemaContractFieldBase: {
	name:         #FieldPath
	type:         #ContractFieldType
	required:     bool | *false
	nullable?:    bool
	description?: string
	validations?: _
}

#SchemaContractField: #SchemaContractFieldBase & ({
	type:         "string"
	validations?: #StringFieldValidations
} | {
	type:         "integer"
	validations?: #IntegerFieldValidations
} | {
	type:         "number"
	validations?: #NumberFieldValidations
} | {
	type: "decimal"
	precision: int & >=1 & <=38
	scale: int & >=0 & <=precision
	validations?: #NumberFieldValidations
} | {
	type:         "boolean"
	validations?: #BooleanFieldValidations
} | {
	type:         "datetime"
	validations?: #DatetimeFieldValidations
} | {
	type:         "array"
	validations?: #ArrayFieldValidations
} | {
	type:         "object"
	validations?: _|_
})

#SchemaContractFieldWithoutUnique: #SchemaContractFieldBase & ({
	type:         "string"
	validations?: #StringFieldValueValidations
} | {
	type:         "integer"
	validations?: #IntegerFieldValueValidations
} | {
	type:         "number"
	validations?: #NumberFieldValueValidations
} | {
	type: "decimal"
	precision: int & >=1 & <=38
	scale: int & >=0 & <=precision
	validations?: #NumberFieldValueValidations
} | {
	type:         "boolean"
	validations?: #BooleanFieldValueValidations
} | {
	type:         "datetime"
	validations?: #DatetimeFieldValueValidations
} | {
	type:         "array"
	validations?: #ArrayFieldValidations
} | {
	type:         "object"
	validations?: _|_
})

#SchemaContractFieldWithoutValidations: #SchemaContractFieldBase & ({
	type:         "string" | "integer" | "number" | "boolean" | "datetime" | "array" | "object"
	validations?: _|_
})

#SchemaContractWithoutMultipart: ({
	name?:          string & !=""
	representation: "file"
	root_shape?:    #ContractRootShape
	fields?: [...#SchemaContractFieldWithoutValidations] & [_, ...]
} | {
	name?:           string & !=""
	representation?: "rows" | "csv"
	root_shape?:     #ContractRootShape
	fields: [...#SchemaContractField] & [_, ...]
} | {
	name?:          string & !=""
	representation: "json"
	root_shape?:    #ContractRootShape
	fields: [...#SchemaContractFieldWithoutUnique] & [_, ...]
})

#SchemaContractWithInferredJSON: {
	name?:           string & !=""
	representation?: *"json" | "json"
	root_shape?:     #ContractRootShape
	fields: [...#SchemaContractFieldWithoutUnique] & [_, ...]
}

#SchemaContract: ({
	name?:          string & !=""
	representation: "file"
	root_shape?:    #ContractRootShape
	fields?: [...#SchemaContractFieldWithoutValidations] & [_, ...]
} | {
	name?:           string & !=""
	representation?: "rows" | "csv"
	root_shape?:     #ContractRootShape
	fields: [...#SchemaContractField] & [_, ...]
} | {
	name?:          string & !=""
	representation: "json"
	root_shape?:    #ContractRootShape
	fields: [...#SchemaContractFieldWithoutUnique] & [_, ...]
} | {
	name?:          string & !=""
	representation: "multipart_form"
	root_shape?:    #ContractRootShape
	fields: [...#SchemaContractFieldWithoutValidations] & [_, ...]
})

#ODCSContractRef: {
	contract_slug:   #Slug
	revision_slug:   #Slug & !="latest"
	schema_object_id: string & !=""
	representation?: #ContractRepresentation
	root_shape?:     #ContractRootShape
}

#TransformSchemaContract: ({
	name?:           string & !=""
	representation?: "rows" | "csv"
	root_shape?:     #ContractRootShape
	fields: [...#SchemaContractField] & [_, ...]
} | {
	name?:          string & !=""
	representation: "json"
	root_shape?:    #ContractRootShape
	fields: [...#SchemaContractFieldWithoutUnique] & [_, ...]
})

// #StepRef is a step output reference resolved at runtime.
// Format: "$steps.<step-slug>.<field>"
// Supported fields:
//   SQL results:     rows, column_names, row_count
//   MongoDB results: documents, document_count, operation_type
//   HTTP results:    status_code, body, headers
//   HubSpot results: rows, column_names, row_count
//   FTP/Git results: content, content_type, byte_size
#StepRef: string & =~"^\\$steps\\.[a-z0-9]+(?:-[a-z0-9]+)*\\.[a-z0-9_]+$"

#ParameterValue: null | bool | number | string | [...#ParameterValue] | {
	[string]: #ParameterValue
}

#IncrementalCheckpointKind: "watermark" | "cursor"

#IncrementalSourceContract: {
	checkpoint_kind?: #IncrementalCheckpointKind
	field?:           string
	cursor?:          string
	lookback_window?: string
	ordering?: [...string]
	dedupe_key?: string
}

#InputStep: {
	kind:  "input"
	slug:  #Slug
	name?: string
	input: ({
		output_contract: #SchemaContract & {representation: "json"}
	} | {
		output_contract_ref: #ODCSContractRef & {
			representation: "json"
			root_shape: #ContractRootShape
		}
	})
}

#QueryStep: {
	kind:  "query"
	slug:  #Slug
	name?: string
	query: {
		data_source_ref: #Slug
		operation_type:   #QueryOperationType
		parameters?: [string]: #ParameterValue
		incremental?: #IncrementalSourceContract
		if operation_type != "mongodb" {
			output_contract?: #SchemaContract
		}
		if operation_type == "mongodb" {
			output_contract?: #SchemaContractWithInferredJSON
		}
		if operation_type != "http" && operation_type != "mongodb" {
			output_contract?: #SchemaContractWithoutMultipart
		}
		output_contract_ref?: #ODCSContractRef
		if output_contract_ref != _|_ {
			output_contract?: null
		}
	} & (#OperationRefTarget | #OperationInlineTarget)
}

#TransformOutputKind: "rows" | "json" | "csv"
#TransformInputName:  string & =~"^[a-zA-Z_][a-zA-Z0-9_]*$"

#InputContractAttachment: {contract: #TransformSchemaContract} | {contract_ref: #ODCSContractRef & {representation?: "rows" | "json" | "csv"}}

#TransformStep: {
	kind:  "transform"
	slug:  #Slug
	name?: string
	transform: {
		inputs: ({[#TransformInputName]: #StepInput}) & struct.MinFields(1)
		sql:              string & !=""
		output_kind:      #TransformOutputKind
		input_contract?:  #TransformSchemaContract
		output_contract?: #TransformSchemaContract
		input_contract_ref?: #ODCSContractRef
		output_contract_ref?: #ODCSContractRef
		input_contracts?: close({for name, _ in inputs { "\(name)"?: #InputContractAttachment }}) & struct.MinFields(1)
		if input_contracts != _|_ {
			input_contract?: null
			input_contract_ref?: null
		}
		if input_contract_ref != _|_ {
			input_contract?: null
		}
		if output_contract_ref != _|_ {
			output_contract?: null
		}
	}
}

#SinkStep: {
	kind:  "sink"
	slug:  #Slug
	name?: string
	sink: {
		input:            #StepInput
		input_contract?:  #SchemaContract
		input_contract_ref?: #ODCSContractRef
		if input_contract_ref != _|_ {
			input_contract?: null
		}
		backend?:         "noop" | "ftp" | "db" | "http" | "mongo" | "s3"
		data_source_ref:  #Slug
		operation_type:   #SinkOperationType
		config?: [string]: _
		empty_input_policy?: "skip" | "reject"
		idempotency_class: "naturally_idempotent" | "idempotent_with_key" | "deduplicated_by_ledger" | "unsafe"
		if operation_type != "http" {
			input_contract?: #SchemaContractWithoutMultipart
		}
	} & (#OperationRefTarget | #OperationInlineTarget)
}
