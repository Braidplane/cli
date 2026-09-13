package nativetype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeJSONLike(t *testing.T) {
	t.Parallel()

	require.Equal(t, TypeInteger, NormalizeJSONLike("int64"))
	require.Equal(t, TypeBoolean, NormalizeJSONLike("bool"))
	require.Equal(t, TypeDatetime, NormalizeJSONLike("date-time"))
	require.Equal(t, TypeObject, NormalizeJSONLike("document"))
	require.Equal(t, "custom_type", NormalizeJSONLike("custom_type"))
}

func TestMergeJSONLike(t *testing.T) {
	t.Parallel()

	require.Equal(t, TypeNumber, MergeJSONLike(TypeInteger, TypeNumber))
	require.Equal(t, TypeInteger, MergeJSONLike(TypeNull, TypeInteger))
	require.Equal(t, TypeBoolean, MergeJSONLike(TypeBoolean, TypeNull))
	require.Equal(t, TypeString, MergeJSONLike(TypeObject, TypeArray))
}
