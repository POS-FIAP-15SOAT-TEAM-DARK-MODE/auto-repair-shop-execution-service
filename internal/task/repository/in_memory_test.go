package repository

import (
	"context"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInMemoryTask(t *testing.T, so string, at time.Time) *task.Task {
	t.Helper()
	n := 0
	tk, err := task.New(func() string { n++; return so + "-" + string(rune('a'+n)) }, so, "saga-"+so, at, []task.NewWork{
		{WorkID: "work-1", Name: "Troca"},
		{WorkID: "work-2", Name: "Filtro"},
	})
	require.NoError(t, err)
	return tk
}

func TestInMemory_InsertFindAndCopySemantics(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()
	tk := newInMemoryTask(t, "so-1", created)
	require.NoError(t, repo.Insert(ctx, nil, tk, tk.TakeChanges()))

	found, err := repo.FindByServiceOrderID(ctx, nil, "so-1")
	require.NoError(t, err)
	assert.Equal(t, task.StatusQueued, found.Status)

	// Changing the returned copy must not change the stored task.
	found.Works[0].Status = work.StatusCancelled
	again, err := repo.FindByServiceOrderID(ctx, nil, "so-1")
	require.NoError(t, err)
	assert.Equal(t, work.StatusAwaitingStart, again.Works[0].Status)

	assert.Len(t, repo.History, 2)
	assert.Nil(t, repo.History[0].Previous)
}

func TestInMemory_FindMissing(t *testing.T) {
	_, err := NewInMemory().FindByServiceOrderID(context.Background(), nil, "nope")
	assert.ErrorIs(t, err, task.ErrTaskNotFound)
}

func TestInMemory_InsertDuplicateServiceOrderFails(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()
	first := newInMemoryTask(t, "so-1", created)
	require.NoError(t, repo.Insert(ctx, nil, first, nil))
	second := newInMemoryTask(t, "so-1", created)
	assert.Error(t, repo.Insert(ctx, nil, second, nil))
}

func TestInMemory_SaveMissingTaskFails(t *testing.T) {
	tk := newInMemoryTask(t, "so-9", created)
	assert.ErrorIs(t, NewInMemory().Save(context.Background(), nil, tk, nil), task.ErrTaskNotFound)
}

func TestInMemory_SaveAppendsHistoryAndUpdates(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()
	tk := newInMemoryTask(t, "so-1", created)
	require.NoError(t, repo.Insert(ctx, nil, tk, tk.TakeChanges()))

	require.NoError(t, tk.Advance("work-1", created))
	require.NoError(t, repo.Save(ctx, nil, tk, tk.TakeChanges()))

	got, err := repo.FindByServiceOrderID(ctx, nil, "so-1")
	require.NoError(t, err)
	assert.Equal(t, task.StatusInProgress, got.Status)
	require.NotNil(t, got.StartedAt)
	require.Len(t, repo.History, 3)
	assert.Equal(t, work.StatusInProgress, repo.History[2].New)
}

func TestInMemory_ListActiveSortedByCreation(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()
	later := newInMemoryTask(t, "so-late", created.Add(time.Hour))
	early := newInMemoryTask(t, "so-early", created)
	done := newInMemoryTask(t, "so-done", created.Add(-time.Hour))
	done.Status = task.StatusCompleted
	for _, tk := range []*task.Task{later, early, done} {
		require.NoError(t, repo.Insert(ctx, nil, tk, nil))
	}

	active, err := repo.ListActive(ctx, nil)
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, "so-early", active[0].ServiceOrderID)
	assert.Equal(t, "so-late", active[1].ServiceOrderID)
}
