package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDB = errors.New("db down")

var taskColumns = []string{"id", "service_order_id", "saga_id", "status", "created_at", "started_at", "completed_at"}
var workColumns = []string{"id", "task_id", "work_id", "name", "status"}

var created = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *Postgres) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database, mock, NewPostgres(id.NewNoopIDGenerator("ulid-1"))
}

func TestPostgres_FindByServiceOrderID(t *testing.T) {
	database, mock, repo := newMock(t)
	started := created.Add(time.Minute)
	mock.ExpectQuery("FROM execution_task WHERE service_order_id").
		WithArgs("so-1").
		WillReturnRows(sqlmock.NewRows(taskColumns).AddRow("t-1", "so-1", "saga-1", "IN_PROGRESS", created, started, nil))
	mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").
		WillReturnRows(sqlmock.NewRows(workColumns).
			AddRow("w-row-1", "t-1", "work-1", "Troca de óleo", "IN_PROGRESS").
			AddRow("w-row-2", "t-1", "work-2", "Filtro", "AWAITING_START"))

	tk, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
	require.NoError(t, err)
	assert.Equal(t, "t-1", tk.ID)
	assert.Equal(t, task.StatusInProgress, tk.Status)
	assert.Equal(t, "saga-1", tk.SagaID)
	require.NotNil(t, tk.StartedAt)
	assert.Equal(t, started, *tk.StartedAt)
	assert.Nil(t, tk.CompletedAt)
	require.Len(t, tk.Works, 2)
	assert.Equal(t, work.StatusInProgress, tk.Works[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_FindByServiceOrderID_CompletedTask(t *testing.T) {
	database, mock, repo := newMock(t)
	completed := created.Add(time.Hour)
	mock.ExpectQuery("FROM execution_task WHERE service_order_id").
		WillReturnRows(sqlmock.NewRows(taskColumns).AddRow("t-1", "so-1", "saga-1", "COMPLETED", created, created, completed))
	mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").
		WillReturnRows(sqlmock.NewRows(workColumns))

	tk, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
	require.NoError(t, err)
	require.NotNil(t, tk.CompletedAt)
	assert.Equal(t, completed, *tk.CompletedAt)
	assert.Empty(t, tk.Works)
}

func TestPostgres_FindByServiceOrderID_NotFound(t *testing.T) {
	database, mock, repo := newMock(t)
	mock.ExpectQuery("FROM execution_task WHERE service_order_id").WillReturnRows(sqlmock.NewRows(taskColumns))

	_, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
	assert.ErrorIs(t, err, task.ErrTaskNotFound)
}

func TestPostgres_FindByServiceOrderID_Errors(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE service_order_id").WillReturnError(errDB)
		_, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
		assert.ErrorIs(t, err, errDB)
	})
	t.Run("scan", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE service_order_id").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("t-1"))
		_, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
		assert.Error(t, err)
	})
	t.Run("works query", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE service_order_id").
			WillReturnRows(sqlmock.NewRows(taskColumns).AddRow("t-1", "so-1", "saga-1", "QUEUED", created, nil, nil))
		mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").WillReturnError(errDB)
		_, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
		assert.ErrorIs(t, err, errDB)
	})
	t.Run("works scan", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE service_order_id").
			WillReturnRows(sqlmock.NewRows(taskColumns).AddRow("t-1", "so-1", "saga-1", "QUEUED", created, nil, nil))
		mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("w-1"))
		_, err := repo.FindByServiceOrderID(context.Background(), database, "so-1")
		assert.Error(t, err)
	})
}

func TestPostgres_ListActive(t *testing.T) {
	database, mock, repo := newMock(t)
	mock.ExpectQuery("FROM execution_task WHERE status IN").
		WillReturnRows(sqlmock.NewRows(taskColumns).
			AddRow("t-1", "so-1", "saga-1", "QUEUED", created, nil, nil).
			AddRow("t-2", "so-2", "saga-2", "IN_PROGRESS", created.Add(time.Minute), created.Add(2*time.Minute), nil))
	mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").
		WillReturnRows(sqlmock.NewRows(workColumns).
			AddRow("w-1", "t-1", "work-1", "Troca", "AWAITING_START").
			AddRow("w-2", "t-2", "work-2", "Filtro", "IN_PROGRESS"))

	tasks, err := repo.ListActive(context.Background(), database)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, "t-1", tasks[0].ID)
	assert.Len(t, tasks[0].Works, 1)
	assert.Equal(t, "t-2", tasks[1].ID)
	assert.Equal(t, work.StatusInProgress, tasks[1].Works[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_ListActive_EmptySkipsWorksQuery(t *testing.T) {
	database, mock, repo := newMock(t)
	mock.ExpectQuery("FROM execution_task WHERE status IN").WillReturnRows(sqlmock.NewRows(taskColumns))

	tasks, err := repo.ListActive(context.Background(), database)
	require.NoError(t, err)
	assert.Empty(t, tasks)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_ListActive_Errors(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE status IN").WillReturnError(errDB)
		_, err := repo.ListActive(context.Background(), database)
		assert.ErrorIs(t, err, errDB)
	})
	t.Run("scan", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE status IN").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("t-1"))
		_, err := repo.ListActive(context.Background(), database)
		assert.Error(t, err)
	})
	t.Run("works query", func(t *testing.T) {
		database, mock, repo := newMock(t)
		mock.ExpectQuery("FROM execution_task WHERE status IN").
			WillReturnRows(sqlmock.NewRows(taskColumns).AddRow("t-1", "so-1", "saga-1", "QUEUED", created, nil, nil))
		mock.ExpectQuery("FROM execution_work WHERE task_id = ANY").WillReturnError(errDB)
		_, err := repo.ListActive(context.Background(), database)
		assert.ErrorIs(t, err, errDB)
	})
}

func newTask(t *testing.T) *task.Task {
	t.Helper()
	n := 0
	tk, err := task.New(func() string { n++; return "id-" + string(rune('0'+n)) }, "so-1", "saga-1", created, []task.NewWork{
		{WorkID: "work-1", Name: "Troca de óleo"},
	})
	require.NoError(t, err)
	return tk
}

func TestPostgres_Insert(t *testing.T) {
	database, mock, repo := newMock(t)
	tk := newTask(t)
	changes := tk.TakeChanges()

	mock.ExpectExec("INSERT INTO execution_task").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO execution_work ").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO execution_work_history").WithArgs("ulid-1", tk.ID, "work-1", nil, "AWAITING_START").
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Insert(context.Background(), database, tk, changes))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_Insert_Errors(t *testing.T) {
	cases := map[string]struct {
		setup func(mock sqlmock.Sqlmock)
	}{
		"task": {setup: func(m sqlmock.Sqlmock) {
			m.ExpectExec("INSERT INTO execution_task").WillReturnError(errDB)
		}},
		"work": {setup: func(m sqlmock.Sqlmock) {
			m.ExpectExec("INSERT INTO execution_task").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectExec("INSERT INTO execution_work ").WillReturnError(errDB)
		}},
		"history": {setup: func(m sqlmock.Sqlmock) {
			m.ExpectExec("INSERT INTO execution_task").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectExec("INSERT INTO execution_work ").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectExec("INSERT INTO execution_work_history").WillReturnError(errDB)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			database, mock, repo := newMock(t)
			tk := newTask(t)
			tc.setup(mock)
			assert.ErrorIs(t, repo.Insert(context.Background(), database, tk, tk.TakeChanges()), errDB)
		})
	}
}

func TestPostgres_Save(t *testing.T) {
	database, mock, repo := newMock(t)
	tk := newTask(t)
	changes := tk.TakeChanges()
	require.NoError(t, tk.Advance("work-1", created))
	changes = append(changes, tk.TakeChanges()...)
	completed := created.Add(time.Hour)
	tk.Status = task.StatusCompleted
	tk.CompletedAt = &completed

	mock.ExpectExec("UPDATE execution_task SET status").WithArgs(tk.ID, task.StatusCompleted, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE execution_work SET status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO execution_work_history").WithArgs("ulid-1", tk.ID, "work-1", nil, "AWAITING_START").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO execution_work_history").WithArgs("ulid-1", tk.ID, "work-1", "AWAITING_START", "IN_PROGRESS").
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Save(context.Background(), database, tk, changes))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_Save_Errors(t *testing.T) {
	cases := map[string]func(mock sqlmock.Sqlmock){
		"task": func(m sqlmock.Sqlmock) {
			m.ExpectExec("UPDATE execution_task").WillReturnError(errDB)
		},
		"work": func(m sqlmock.Sqlmock) {
			m.ExpectExec("UPDATE execution_task").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("UPDATE execution_work").WillReturnError(errDB)
		},
		"history": func(m sqlmock.Sqlmock) {
			m.ExpectExec("UPDATE execution_task").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("UPDATE execution_work").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("INSERT INTO execution_work_history").WillReturnError(errDB)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			database, mock, repo := newMock(t)
			tk := newTask(t)
			require.NoError(t, tk.CancelWork("work-1"))
			setup(mock)
			assert.ErrorIs(t, repo.Save(context.Background(), database, tk, tk.TakeChanges()), errDB)
		})
	}
}
