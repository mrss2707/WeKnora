package handler

import (
	"errors"
	"net/http"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMemoryWriteModelError_Is503WithoutProviderDetails(t *testing.T) {
	c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", nil)
	cause := errors.New("upstream http://private-model.invalid/?api_key=not-a-real-key failed")
	(&MemoryV2Handler{}).writeMemoryError(c, &types.MemoryV2ModelUnavailable{Cause: cause}, "Failed to save memory", "tenant", "")
	require.Len(t, c.Errors, 1)
	var app *apperrors.AppError
	require.ErrorAs(t, c.Errors.Last().Err, &app)
	require.Equal(t, http.StatusServiceUnavailable, app.HTTPCode)
	require.Contains(t, app.Message, "embedding model")
	require.NotContains(t, app.Message, "private-model")
	require.NotContains(t, app.Message, "api_key")
}
