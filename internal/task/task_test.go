package task

import (
	"fmt"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func seqID() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("id-%d", n)
	}
}

// newTask builds a QUEUED task with two works: w-1 "Troca de óleo" and w-2 "Filtro".
func newTask(t *testing.T) *Task {
	t.Helper()
	tk, err := New(seqID(), "so-1", "saga-1", now, []NewWork{
		{WorkID: "w-1", Name: "Troca de óleo"},
		{WorkID: "w-2", Name: "Filtro"},
	})
	require.NoError(t, err)
	return tk
}

func TestNew_QueuedWithAwaitingWorksAndChanges(t *testing.T) {
	tk := newTask(t)

	assert.Equal(t, StatusQueued, tk.Status)
	assert.Equal(t, "so-1", tk.ServiceOrderID)
	assert.Equal(t, "saga-1", tk.SagaID)
	assert.Equal(t, now, tk.CreatedAt)
	assert.Nil(t, tk.StartedAt)
	assert.Nil(t, tk.CompletedAt)
	require.Len(t, tk.Works, 2)
	for _, w := range tk.Works {
		assert.Equal(t, work.StatusAwaitingStart, w.Status)
		assert.Equal(t, tk.ID, w.TaskID)
	}

	changes := tk.TakeChanges()
	require.Len(t, changes, 2)
	assert.Nil(t, changes[0].Previous, "creation has no previous status")
	assert.Equal(t, "w-1", changes[0].WorkID)
	assert.Equal(t, "Troca de óleo", changes[0].Name)
	assert.Equal(t, work.StatusAwaitingStart, changes[0].New)
	assert.Empty(t, tk.TakeChanges(), "changes are cleared once taken")
}

func TestNew_RejectsInvalidWorks(t *testing.T) {
	cases := map[string][]NewWork{
		"no works":          {},
		"nil works":         nil,
		"empty work id":     {{WorkID: "", Name: "x"}},
		"repeated work id":  {{WorkID: "w-1", Name: "a"}, {WorkID: "w-1", Name: "b"}},
		"empty among valid": {{WorkID: "w-1", Name: "a"}, {WorkID: "", Name: "b"}},
	}
	for name, works := range cases {
		t.Run(name, func(t *testing.T) {
			tk, err := New(seqID(), "so-1", "saga-1", now, works)
			assert.ErrorIs(t, err, ErrInvalidWorks)
			assert.Nil(t, tk)
		})
	}
}

func TestStatus_Active(t *testing.T) {
	assert.True(t, StatusQueued.Active())
	assert.True(t, StatusInProgress.Active())
	assert.False(t, StatusCompleted.Active())
	assert.False(t, StatusCancelled.Active())
}

func TestAdvance_FirstStartMovesTaskToInProgress(t *testing.T) {
	tk := newTask(t)
	tk.TakeChanges()
	later := now.Add(time.Hour)

	require.NoError(t, tk.Advance("w-1", later))
	assert.Equal(t, StatusInProgress, tk.Status)
	require.NotNil(t, tk.StartedAt)
	assert.Equal(t, later, *tk.StartedAt)
	assert.Equal(t, work.StatusInProgress, tk.Works[0].Status)

	// A second work starting does not move startedAt again.
	require.NoError(t, tk.Advance("w-2", later.Add(time.Hour)))
	assert.Equal(t, later, *tk.StartedAt)

	changes := tk.TakeChanges()
	require.Len(t, changes, 2)
	assert.Equal(t, work.StatusAwaitingStart, *changes[0].Previous)
	assert.Equal(t, work.StatusInProgress, changes[0].New)
}

func TestAdvance_Errors(t *testing.T) {
	tk := newTask(t)
	assert.ErrorIs(t, tk.Advance("missing", now), work.ErrWorkServiceOrderNotFound)

	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now))
	assert.ErrorIs(t, tk.Advance("w-1", now), work.ErrWorkServiceOrderAlreadyCompleted)

	require.NoError(t, tk.CancelWork("w-2"))
	assert.ErrorIs(t, tk.Advance("w-2", now), work.ErrWorkServiceOrderAlreadyCancelled)

	tk.Status = StatusCompleted
	assert.ErrorIs(t, tk.Advance("w-1", now), ErrTaskNotActive)
}

func TestAdvance_WorkCompletionKeepsTaskInProgress(t *testing.T) {
	// A work reaches COMPLETED only after IN_PROGRESS, so the task is already IN_PROGRESS.
	tk := newTask(t)
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now))
	assert.Equal(t, StatusInProgress, tk.Status)
}

func TestCancelWork(t *testing.T) {
	tk := newTask(t)
	tk.TakeChanges()

	require.NoError(t, tk.CancelWork("w-2"))
	assert.Equal(t, work.StatusCancelled, tk.Works[1].Status)
	assert.Equal(t, StatusQueued, tk.Status, "cancelling a work does not change the task")

	changes := tk.TakeChanges()
	require.Len(t, changes, 1)
	assert.Equal(t, "w-2", changes[0].WorkID)
	assert.Equal(t, work.StatusCancelled, changes[0].New)
}

func TestCancelWork_Errors(t *testing.T) {
	tk := newTask(t)
	assert.ErrorIs(t, tk.CancelWork("missing"), work.ErrWorkServiceOrderNotFound)

	require.NoError(t, tk.CancelWork("w-1"))
	assert.ErrorIs(t, tk.CancelWork("w-1"), work.ErrWorkServiceOrderAlreadyCancelled)

	require.NoError(t, tk.Advance("w-2", now))
	require.NoError(t, tk.Advance("w-2", now))
	assert.ErrorIs(t, tk.CancelWork("w-2"), work.ErrWorkServiceOrderAlreadyCompleted)

	tk.Status = StatusCancelled
	assert.ErrorIs(t, tk.CancelWork("w-2"), ErrTaskNotActive)
}

func TestCancelAll_CancelsNonTerminalWorksAndTask(t *testing.T) {
	tk := newTask(t)
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now)) // w-1 COMPLETED
	tk.TakeChanges()

	require.NoError(t, tk.CancelAll())
	assert.Equal(t, StatusCancelled, tk.Status)
	assert.Equal(t, work.StatusCompleted, tk.Works[0].Status, "terminal works are kept")
	assert.Equal(t, work.StatusCancelled, tk.Works[1].Status)

	changes := tk.TakeChanges()
	require.Len(t, changes, 1)
	assert.Equal(t, "w-2", changes[0].WorkID)
}

func TestCancelAll_NotActive(t *testing.T) {
	tk := newTask(t)
	tk.Status = StatusCompleted
	assert.ErrorIs(t, tk.CancelAll(), ErrTaskNotActive)
}

func TestFinish_RequiresEveryWorkTerminal(t *testing.T) {
	tk := newTask(t)
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now))

	outcome, err := tk.Finish(now)
	assert.ErrorIs(t, err, ErrWorksNotFinished)
	assert.Empty(t, outcome)
	assert.Equal(t, StatusInProgress, tk.Status)
	assert.Nil(t, tk.CompletedAt)
}

func TestFinish_AllCompleted(t *testing.T) {
	tk := newTask(t)
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-2", now))
	require.NoError(t, tk.Advance("w-2", now))

	later := now.Add(2 * time.Hour)
	outcome, err := tk.Finish(later)
	require.NoError(t, err)
	assert.Equal(t, FinishCompleted, outcome)
	assert.Equal(t, StatusCompleted, tk.Status)
	require.NotNil(t, tk.CompletedAt)
	assert.Equal(t, later, *tk.CompletedAt)
}

func TestFinish_MixOfCompletedAndCancelledCompletesTask(t *testing.T) {
	tk := newTask(t)
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.Advance("w-1", now))
	require.NoError(t, tk.CancelWork("w-2"))

	outcome, err := tk.Finish(now)
	require.NoError(t, err)
	assert.Equal(t, FinishCompleted, outcome)
	assert.Equal(t, StatusCompleted, tk.Status)
}

func TestFinish_AllCancelledCancelsTask(t *testing.T) {
	tk := newTask(t)
	require.NoError(t, tk.CancelWork("w-1"))
	require.NoError(t, tk.CancelWork("w-2"))

	outcome, err := tk.Finish(now)
	require.NoError(t, err)
	assert.Equal(t, FinishAllCancelled, outcome)
	assert.Equal(t, StatusCancelled, tk.Status)
	assert.Nil(t, tk.CompletedAt)
}

func TestFinish_NotActive(t *testing.T) {
	tk := newTask(t)
	tk.Status = StatusCompleted
	_, err := tk.Finish(now)
	assert.ErrorIs(t, err, ErrTaskNotActive)
}

func TestFindWork(t *testing.T) {
	tk := newTask(t)
	w, err := tk.findWork("w-2")
	require.NoError(t, err)
	assert.Equal(t, "Filtro", w.Name)

	_, err = tk.findWork("nope")
	assert.ErrorIs(t, err, work.ErrWorkServiceOrderNotFound)
}
