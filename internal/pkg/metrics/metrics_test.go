package metrics

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gatherGauge(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == name {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestRegisterOutboxPending_ReadsCountOnEachScrape(t *testing.T) {
	reg := prometheus.NewRegistry()
	n := int64(3)
	require.NoError(t, RegisterOutboxPending(reg, func(context.Context) (int64, error) { return n, nil }))

	assert.Equal(t, 3.0, gatherGauge(t, reg, "outbox_pending"))
	n = 0
	assert.Equal(t, 0.0, gatherGauge(t, reg, "outbox_pending"))
}

func TestRegisterOutboxPending_ErrorIsNaN(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NoError(t, RegisterOutboxPending(reg, func(context.Context) (int64, error) {
		return 0, errors.New("db down")
	}))

	assert.True(t, math.IsNaN(gatherGauge(t, reg, "outbox_pending")))
}

func TestRegisterOutboxPending_TwiceFails(t *testing.T) {
	reg := prometheus.NewRegistry()
	count := func(context.Context) (int64, error) { return 0, nil }
	require.NoError(t, RegisterOutboxPending(reg, count))
	assert.Error(t, RegisterOutboxPending(reg, count))
}
