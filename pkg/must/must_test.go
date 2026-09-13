package must

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFn(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsValueWhenNoError", func(t *testing.T) {
		t.Parallel()

		result := Fn("success", nil)
		assert.Equal(t, "success", result)
	})

	t.Run("PanicsWhenError", func(t *testing.T) {
		t.Parallel()

		testErr := fmt.Errorf("test error")
		assert.PanicsWithError(t, "test error", func() {
			Fn("value", testErr)
		})
	})

	t.Run("WorksWithDifferentTypes", func(t *testing.T) {
		t.Parallel()

		intResult := Fn(42, nil)
		assert.Equal(t, 42, intResult)

		boolResult := Fn(true, nil)
		assert.True(t, boolResult)
	})
}

func TestFnErr(t *testing.T) {
	t.Parallel()

	t.Run("DoesNotPanicWhenNoError", func(t *testing.T) {
		t.Parallel()

		assert.NotPanics(t, func() {
			FnErr(nil)
		})
	})

	t.Run("PanicsWhenError", func(t *testing.T) {
		t.Parallel()

		testErr := fmt.Errorf("test error")
		assert.PanicsWithError(t, "test error", func() {
			FnErr(testErr)
		})
	})
}
