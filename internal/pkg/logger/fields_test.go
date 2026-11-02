package logger

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestInitLogger_CarriesServiceField(t *testing.T) {
	t.Setenv("LOG_LEVEL", "INFO")
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	initLogger().Info("probe")
	require.NoError(t, w.Close())
	os.Stderr = stderr

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"service":"execution-service"`)
	assert.Contains(t, string(out), `"message":"probe"`)
}

func TestWith_AddsFieldsAndRedactsSensitiveOnes(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := withContext(context.Background(), &customLogger{zap.New(core)})

	ctx = With(ctx,
		zap.String("saga_id", "saga-1"),
		zap.String("service_order_id", "so-1"),
		zap.String("jwt", "eyJhbGciOi.secret"),
		zap.String("cpf", "12345678901"),
	)
	Of(ctx).Info("execution.probe", zap.String("message_id", "msg-1"))

	entries := logs.All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	assert.Equal(t, "saga-1", fields["saga_id"])
	assert.Equal(t, "so-1", fields["service_order_id"])
	assert.Equal(t, "msg-1", fields["message_id"])
	assert.Equal(t, "[REDACTED]", fields["jwt"])
	assert.Equal(t, "[REDACTED]", fields["cpf"])
}

func TestWith_KeepsFieldsOfParentLogger(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := withContext(context.Background(), &customLogger{zap.New(core).With(zap.String("request_id", "req-1"))})

	ctx = With(ctx, zap.String("message_id", "msg-2"))
	Of(ctx).Info("message.probe")

	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "req-1", fields["request_id"])
	assert.Equal(t, "msg-2", fields["message_id"])
}

func TestRequest_UsesGlobalLoggerWithRequestID(t *testing.T) {
	ctx, reqId := Request(context.Background(), "req-xyz")
	assert.Equal(t, "req-xyz", reqId)
	assert.NotEqual(t, Global(), Of(ctx))
	assert.False(t, strings.Contains(reqId, " "))
}
