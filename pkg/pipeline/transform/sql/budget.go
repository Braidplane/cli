package sql

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Braidplane/cli/pkg/decimal"
)

func maxInt() int {
	return int(^uint(0) >> 1)
}

func chargeInputRows(rows []map[string]any, budget *executionBudget) error {
	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return err
		}
		size, err := estimateTransformRowBytes(row, budget, 1)
		if err != nil {
			return err
		}
		if err := budget.chargeIntermediate(1, size); err != nil {
			return err
		}
	}
	return nil
}

func estimateTransformRowsBytes(rows []map[string]any, budget *executionBudget) (int64, error) {
	total := int64(2)
	for _, row := range rows {
		if err := budget.checkContext(); err != nil {
			return 0, err
		}
		size, err := estimateTransformRowBytes(row, budget, 1)
		if err != nil {
			return 0, err
		}
		total, err = checkedAddSize(total, size+1, ErrOutputBytesExceeded)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func estimateTransformRowBytes(
	row map[string]any,
	budget *executionBudget,
	depth int,
) (int64, error) {
	if err := budget.checkDepth(depth); err != nil {
		return 0, err
	}
	total := int64(2)
	for key, value := range row {
		if err := budget.checkContext(); err != nil {
			return 0, err
		}
		valueSize, err := estimateTransformValueBytes(value, budget, depth+1)
		if err != nil {
			return 0, err
		}
		fieldSize := int64(len(key)) + valueSize + 4
		total, err = checkedAddSize(total, fieldSize, ErrIntermediateBytesExceeded)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func estimateTransformValueBytes(
	value any,
	budget *executionBudget,
	depth int,
) (int64, error) {
	if err := budget.checkDepth(depth); err != nil {
		return 0, err
	}

	switch typed := value.(type) {
	case decimal.Value:
		return typed.EstimatedBytes(), nil
	case nil:
		return 4, nil
	case string:
		return int64(len(typed)) + 2, nil
	case []byte:
		return int64(len(typed)), nil
	case bool:
		if typed {
			return 4, nil
		}
		return 5, nil
	case json.Number:
		return int64(len(typed)), nil
	case int:
		return int64(len(strconv.Itoa(typed))), nil
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return int64(len(fmt.Sprint(typed))), nil
	case time.Time:
		return int64(len(typed.Format(time.RFC3339Nano))) + 2, nil
	case transformJSONConstructedValue:
		return estimateTransformValueBytes(typed.value, budget, depth+1)
	case map[string]any:
		return estimateTransformRowBytes(typed, budget, depth+1)
	case []any:
		total := int64(2)
		for _, item := range typed {
			itemSize, err := estimateTransformValueBytes(item, budget, depth+1)
			if err != nil {
				return 0, err
			}
			total, err = checkedAddSize(total, itemSize+1, ErrIntermediateBytesExceeded)
			if err != nil {
				return 0, err
			}
		}
		return total, nil
	default:
		return int64(len(fmt.Sprint(typed))), nil
	}
}

func estimateQualifiedTransformRowBytes(
	row map[string]any,
	qualifier string,
	budget *executionBudget,
) (int64, error) {
	if err := budget.checkDepth(1); err != nil {
		return 0, err
	}
	total := int64(2)
	for key, value := range row {
		valueSize, err := estimateTransformValueBytes(value, budget, 2)
		if err != nil {
			return 0, err
		}
		fieldSize := int64(len(key)+len(qualifier)+len(key)+1) + 2*valueSize + 8
		total, err = checkedAddSize(total, fieldSize, ErrIntermediateBytesExceeded)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func estimateMergedTransformRowBytes(
	left map[string]any,
	right map[string]any,
	budget *executionBudget,
) (int64, error) {
	leftSize, err := estimateTransformRowBytes(left, budget, 1)
	if err != nil {
		return 0, err
	}
	rightSize, err := estimateTransformRowBytes(right, budget, 1)
	if err != nil {
		return 0, err
	}
	return checkedAddSize(leftSize, rightSize, ErrIntermediateBytesExceeded)
}

func checkOutputBytes(size int64, budget *executionBudget) error {
	if err := budget.checkContext(); err != nil {
		return err
	}
	if budget.limits.MaxOutputBytes > 0 && size > budget.limits.MaxOutputBytes {
		budget.outputBytes = size
		return ErrOutputBytesExceeded.Detailf("maximum output bytes is %d", budget.limits.MaxOutputBytes)
	}
	budget.outputBytes = size
	return nil
}

func chargeDecimalValue(value any, budget *executionBudget) (any, error) {
	if typed, ok := value.(decimal.Value); ok && budget != nil {
		if typed.Rounded() {
			budget.roundedDecimalValues++
		}
		if typed.Inexact() {
			budget.inexactDecimalValues++
		}
		if err := budget.chargeWork(1); err != nil {
			return nil, err
		}
		if err := budget.chargeIntermediate(0, typed.EstimatedBytes()); err != nil {
			return nil, err
		}
	}
	return value, nil
}
