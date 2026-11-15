package work

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatus_IsTerminal(t *testing.T) {
	assert.False(t, StatusAwaitingStart.IsTerminal())
	assert.False(t, StatusInProgress.IsTerminal())
	assert.True(t, StatusCompleted.IsTerminal())
	assert.True(t, StatusCancelled.IsTerminal())
}

func TestNextStatus(t *testing.T) {
	cases := map[string]struct {
		from Status
		want Status
		err  error
	}{
		"awaiting start -> in progress": {from: StatusAwaitingStart, want: StatusInProgress},
		"in progress -> completed":      {from: StatusInProgress, want: StatusCompleted},
		"completed has no next":         {from: StatusCompleted, err: ErrWorkServiceOrderAlreadyCompleted},
		"cancelled has no next":         {from: StatusCancelled, err: ErrWorkServiceOrderAlreadyCancelled},
		"unknown status is invalid":     {from: Status("BOGUS"), err: ErrWorkServiceOrderInvalidStatus},
		"empty status is invalid":       {from: "", err: ErrWorkServiceOrderInvalidStatus},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := &Work{Status: tc.from}
			got, err := w.NextStatus()
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAdvance_RecordsChange(t *testing.T) {
	w := &Work{WorkID: "w-1", Name: "Troca de óleo", Status: StatusAwaitingStart}

	ch, err := w.Advance()
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, w.Status)
	assert.Equal(t, "w-1", ch.WorkID)
	assert.Equal(t, "Troca de óleo", ch.Name)
	require.NotNil(t, ch.Previous)
	assert.Equal(t, StatusAwaitingStart, *ch.Previous)
	assert.Equal(t, StatusInProgress, ch.New)

	ch, err = w.Advance()
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, w.Status)
	assert.Equal(t, StatusInProgress, *ch.Previous)

	_, err = w.Advance()
	assert.ErrorIs(t, err, ErrWorkServiceOrderAlreadyCompleted)
	assert.Equal(t, StatusCompleted, w.Status)
}

func TestAdvance_ErrorLeavesWorkUnchanged(t *testing.T) {
	w := &Work{Status: StatusCancelled}
	_, err := w.Advance()
	assert.ErrorIs(t, err, ErrWorkServiceOrderAlreadyCancelled)
	assert.Equal(t, StatusCancelled, w.Status)
}

func TestCancel(t *testing.T) {
	cases := map[string]struct {
		from Status
		err  error
	}{
		"awaiting start can be cancelled": {from: StatusAwaitingStart},
		"in progress can be cancelled":    {from: StatusInProgress},
		"completed cannot be cancelled":   {from: StatusCompleted, err: ErrWorkServiceOrderAlreadyCompleted},
		"cancelled cannot be cancelled":   {from: StatusCancelled, err: ErrWorkServiceOrderAlreadyCancelled},
		"unknown status is invalid":       {from: Status("BOGUS"), err: ErrWorkServiceOrderInvalidStatus},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := &Work{WorkID: "w-1", Name: "Filtro", Status: tc.from}
			ch, err := w.Cancel()
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				assert.Equal(t, tc.from, w.Status)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, StatusCancelled, w.Status)
			assert.Equal(t, StatusCancelled, ch.New)
			assert.Equal(t, tc.from, *ch.Previous)
		})
	}
}
