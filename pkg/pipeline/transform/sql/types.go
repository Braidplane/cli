package sql

import (
	"context"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

var (
	ErrRequired          = xerror.TransformSQLRequired()
	ErrNotSelect         = xerror.TransformSQLNotSelect()
	ErrMultiStatement    = xerror.TransformSQLMultiStatement()
	ErrFeatureNotAllowed = xerror.TransformSQLFeatureNotAllowed()
	ErrContractInvalid   = xerror.TransformContractInvalid()
	ErrContractType      = xerror.TransformContractTypeMismatch()
	ErrContractMissing   = xerror.TransformContractMissingField()
	ErrTypeInput         = xerror.TransformTypeValidationInput()

	ErrRuntimeProjectionExpressionUnsupported   = xerror.TransformRuntimeProjectionExpressionUnsupported()
	ErrRuntimeProjectionFieldNotFound           = xerror.TransformRuntimeProjectionFieldNotFound()
	ErrRuntimeAggregateExpressionUnsupported    = xerror.TransformRuntimeAggregateExpressionUnsupported()
	ErrRuntimeAggregateColumnReference          = xerror.InvalidTransformRuntimeAggregateColumnReferenceRequired()
	ErrRuntimeWhereExpressionUnsupported        = xerror.TransformRuntimeWhereExpressionUnsupported()
	ErrRuntimeOutputKindUnsupported             = xerror.TransformRuntimeOutputKindUnsupported()
	ErrRuntimeHeadlessCSVNamedAccessUnsupported = xerror.TransformRuntimeHeadlessCSVNamedAccessUnsupported()
	ErrOutputContractViolation                  = xerror.TransformOutputContractViolation()

	ErrInvalidRuntimeLimits       = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_LIMITS_INVALID", "transform runtime limits are invalid")
	ErrWorkBudgetExceeded         = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_WORK_BUDGET_EXCEEDED", "transform work budget exceeded")
	ErrIntermediateRowsExceeded   = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_INTERMEDIATE_ROWS_BUDGET_EXCEEDED", "transform intermediate row budget exceeded")
	ErrIntermediateBytesExceeded  = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_INTERMEDIATE_BYTES_BUDGET_EXCEEDED", "transform intermediate byte budget exceeded")
	ErrOutputRowsExceeded         = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_OUTPUT_ROWS_BUDGET_EXCEEDED", "transform output row budget exceeded")
	ErrOutputBytesExceeded        = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_OUTPUT_BYTES_BUDGET_EXCEEDED", "transform output byte budget exceeded")
	ErrDepthBudgetExceeded        = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_DEPTH_BUDGET_EXCEEDED", "transform depth budget exceeded")
	ErrExecutionBudgetInterrupted = xerror.New(xerror.KindTimeout, "PIPELINE_TRANSFORM_EXECUTION_INTERRUPTED", "transform timeout or cancellation budget exceeded")
)

type Transform struct {
	SQL            string
	OutputKind     model.TransformOutputKind
	InputContract  *pipepayload.Contract
	InputContracts map[string]*pipepayload.Contract
	OutputContract *pipepayload.Contract
}

// Limits bounds one transform execution. Zero values leave the corresponding
// core limit disabled so callers can preserve the legacy runtime while
// deployment configuration supplies positive deny-by-default limits.
type Limits struct {
	MaxWork              int64
	MaxIntermediateRows  int
	MaxIntermediateBytes int64
	MaxOutputRows        int
	MaxOutputBytes       int64
	MaxDepth             int
	Timeout              time.Duration
}

// Usage contains only aggregate execution measurements safe for metrics.
type Usage struct {
	Work                 int64
	IntermediateRows     int64
	IntermediateBytes    int64
	OutputRows           int
	OutputBytes          int64
	MaxDepth             int
	Duration             time.Duration
	RoundedDecimalValues int64
	InexactDecimalValues int64
}

// Observer receives aggregate usage and the final error without payload data.
type Observer func(Usage, error)

type Runtime struct {
	limits  Limits
	initErr error
	observe Observer
}

// NewRuntime creates a transform runtime. The variadic parameter preserves
// source compatibility with the original no-argument constructor.
func NewRuntime(configured ...Limits) Runtime {
	if len(configured) > 1 {
		return Runtime{initErr: ErrInvalidRuntimeLimits.Detail("only one limits value is allowed")}
	}

	limits := Limits{}
	if len(configured) == 1 {
		limits = configured[0]
	}
	if err := validateLimits(limits); err != nil {
		return Runtime{limits: limits, initErr: err}
	}

	return Runtime{limits: limits}
}

// WithObserver returns a runtime that reports aggregate usage after execution.
func (r Runtime) WithObserver(observer Observer) Runtime {
	r.observe = observer
	return r
}

func (r Runtime) Execute(
	transform Transform,
	upstreamPayloads map[string]*pipepayload.Payload,
) (*pipepayload.Output, error) {
	return r.ExecuteContext(context.Background(), transform, upstreamPayloads)
}

// ExecuteContext executes a transform with cumulative per-execution budgets.
func (r Runtime) ExecuteContext(
	ctx context.Context,
	transform Transform,
	upstreamPayloads map[string]*pipepayload.Payload,
) (output *pipepayload.Output, err error) {
	if r.initErr != nil {
		return nil, r.initErr
	}
	if ctx == nil {
		return nil, ErrExecutionBudgetInterrupted.Detail("execution context is required")
	}

	if r.limits.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.limits.Timeout)
		defer cancel()
	}

	budget := newExecutionBudget(ctx, r.limits)
	startedAt := time.Now()
	defer func() {
		if r.observe != nil {
			usage := budget.usage()
			usage.Duration = time.Since(startedAt)
			r.observe(usage, err)
		}
	}()
	return executeTransformStepFromPayloadsWithBudget(&transform, upstreamPayloads, budget)
}

func validateLimits(limits Limits) error {
	if limits.MaxWork < 0 {
		return ErrInvalidRuntimeLimits.Detail("max work must be non-negative")
	}
	if limits.MaxIntermediateRows < 0 {
		return ErrInvalidRuntimeLimits.Detail("max intermediate rows must be non-negative")
	}
	if limits.MaxIntermediateBytes < 0 {
		return ErrInvalidRuntimeLimits.Detail("max intermediate bytes must be non-negative")
	}
	if limits.MaxOutputRows < 0 {
		return ErrInvalidRuntimeLimits.Detail("max output rows must be non-negative")
	}
	if limits.MaxOutputBytes < 0 {
		return ErrInvalidRuntimeLimits.Detail("max output bytes must be non-negative")
	}
	if limits.MaxDepth < 0 {
		return ErrInvalidRuntimeLimits.Detail("max depth must be non-negative")
	}
	if limits.Timeout < 0 {
		return ErrInvalidRuntimeLimits.Detail("timeout must be non-negative")
	}
	return nil
}

type executionBudget struct {
	ctx                  context.Context
	limits               Limits
	work                 int64
	intermediateRows     int64
	intermediateSize     int64
	outputRows           int
	outputBytes          int64
	maxDepth             int
	roundedDecimalValues int64
	inexactDecimalValues int64
}

func newExecutionBudget(ctx context.Context, limits Limits) *executionBudget {
	return &executionBudget{ctx: ctx, limits: limits}
}

func (b *executionBudget) checkContext() error {
	if b == nil || b.ctx == nil {
		return nil
	}
	select {
	case <-b.ctx.Done():
		return ErrExecutionBudgetInterrupted.Detail(b.ctx.Err().Error())
	default:
		return nil
	}
}

func (b *executionBudget) checkDepth(depth int) error {
	if err := b.checkContext(); err != nil {
		return err
	}
	if b != nil && b.limits.MaxDepth > 0 && depth > b.limits.MaxDepth {
		return ErrDepthBudgetExceeded.Detailf("maximum depth is %d", b.limits.MaxDepth)
	}
	if b != nil && depth > b.maxDepth {
		b.maxDepth = depth
	}
	return nil
}

func (b *executionBudget) usage() Usage {
	if b == nil {
		return Usage{}
	}
	return Usage{
		Work:                 b.work,
		IntermediateRows:     b.intermediateRows,
		IntermediateBytes:    b.intermediateSize,
		OutputRows:           b.outputRows,
		OutputBytes:          b.outputBytes,
		MaxDepth:             b.maxDepth,
		RoundedDecimalValues: b.roundedDecimalValues,
		InexactDecimalValues: b.inexactDecimalValues,
	}
}

func (b *executionBudget) chargeWork(amount int64) error {
	if err := b.checkContext(); err != nil {
		return err
	}
	if amount < 0 || b.work > int64(^uint64(0)>>1)-amount {
		return ErrWorkBudgetExceeded.Detail("work accounting overflow")
	}
	next := b.work + amount
	if b.limits.MaxWork > 0 && next > b.limits.MaxWork {
		b.work = next
		return ErrWorkBudgetExceeded.Detailf("maximum work is %d candidate operations", b.limits.MaxWork)
	}
	b.work = next
	return nil
}

func (b *executionBudget) checkExpression(depth int) error {
	if err := b.checkContext(); err != nil {
		return err
	}
	return b.checkDepth(depth)
}

func (b *executionBudget) chargeIntermediate(rows, size int64) error {
	if err := b.checkContext(); err != nil {
		return err
	}
	if rows < 0 || b.intermediateRows > int64(^uint64(0)>>1)-rows {
		return ErrIntermediateRowsExceeded.Detail("intermediate row accounting overflow")
	}
	if size < 0 || b.intermediateSize > int64(^uint64(0)>>1)-size {
		return ErrIntermediateBytesExceeded.Detail("intermediate byte accounting overflow")
	}
	if b.limits.MaxIntermediateRows > 0 && rows > int64(b.limits.MaxIntermediateRows)-b.intermediateRows {
		return ErrIntermediateRowsExceeded.Detailf("maximum cumulative intermediate rows is %d", b.limits.MaxIntermediateRows)
	}
	if b.limits.MaxIntermediateBytes > 0 && size > b.limits.MaxIntermediateBytes-b.intermediateSize {
		return ErrIntermediateBytesExceeded.Detailf("maximum cumulative intermediate bytes is %d", b.limits.MaxIntermediateBytes)
	}
	b.intermediateRows += rows
	b.intermediateSize += size
	return nil
}

func checkedProduct(left, right int) (int64, error) {
	if left < 0 || right < 0 {
		return 0, ErrWorkBudgetExceeded.Detail("negative join cardinality")
	}
	if left == 0 || right == 0 {
		return 0, nil
	}
	if int64(left) > int64(^uint64(0)>>1)/int64(right) {
		return 0, ErrWorkBudgetExceeded.Detail("join candidate cardinality overflow")
	}
	return int64(left) * int64(right), nil
}

func checkedAddSize(total, amount int64, budgetType *xerror.Error) (int64, error) {
	if amount < 0 || total > int64(^uint64(0)>>1)-amount {
		return 0, budgetType.Detail("byte accounting overflow")
	}
	return total + amount, nil
}

func isCompatibleContractFieldType(
	producer model.ContractFieldType,
	consumer model.ContractFieldType,
) bool {
	if producer == consumer {
		return true
	}

	return (consumer == model.ContractFieldTypeValueNumber || consumer == model.ContractFieldTypeValueDecimal) &&
		producer == model.ContractFieldTypeValueInteger
}
