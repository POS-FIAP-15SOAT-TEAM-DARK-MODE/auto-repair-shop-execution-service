package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/db"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/lib/pq"
)

const (
	selectTaskByServiceOrderQuery = `SELECT id, service_order_id, saga_id, status, created_at, started_at, completed_at
		FROM execution_task WHERE service_order_id = $1`
	selectActiveTasksQuery = `SELECT id, service_order_id, saga_id, status, created_at, started_at, completed_at
		FROM execution_task WHERE status IN ('QUEUED', 'IN_PROGRESS') ORDER BY created_at, id`
	selectWorksByTaskQuery = `SELECT id, task_id, work_id, name, status FROM execution_work
		WHERE task_id = ANY($1) ORDER BY task_id, name, work_id`
	insertTaskQuery = `INSERT INTO execution_task (id, service_order_id, saga_id, status, created_at, started_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	insertWorkQuery  = `INSERT INTO execution_work (id, task_id, work_id, name, status) VALUES ($1, $2, $3, $4, $5)`
	updateTaskQuery  = `UPDATE execution_task SET status = $2, started_at = $3, completed_at = $4 WHERE id = $1`
	updateWorkQuery  = `UPDATE execution_work SET status = $2 WHERE id = $1`
	insertHistoryQry = `INSERT INTO execution_work_history (id, task_id, work_id, previous_status, new_status)
		VALUES ($1, $2, $3, $4, $5)`
)

// Postgres implements task.Repository on the execution_task, execution_work and execution_work_history tables.
type Postgres struct {
	ids id.Generator
}

func NewPostgres(ids id.Generator) *Postgres {
	return &Postgres{ids: ids}
}

func (r *Postgres) FindByServiceOrderID(ctx context.Context, ex db.Executor, serviceOrderID string) (*task.Task, error) {
	row := ex.QueryRowContext(ctx, selectTaskByServiceOrderQuery, serviceOrderID)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	works, err := r.worksByTask(ctx, ex, []string{t.ID})
	if err != nil {
		return nil, err
	}
	t.Works = works[t.ID]
	return t, nil
}

func (r *Postgres) ListActive(ctx context.Context, ex db.Executor) ([]*task.Task, error) {
	rows, err := ex.QueryContext(ctx, selectActiveTasksQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []*task.Task
	var ids []string
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
		ids = append(ids, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}

	works, err := r.worksByTask(ctx, ex, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		t.Works = works[t.ID]
	}
	return tasks, nil
}

func (r *Postgres) Insert(ctx context.Context, ex db.Executor, t *task.Task, changes []work.Change) error {
	if _, err := ex.ExecContext(ctx, insertTaskQuery, t.ID, t.ServiceOrderID, t.SagaID, t.Status,
		t.CreatedAt, nullableTime(t.StartedAt), nullableTime(t.CompletedAt)); err != nil {
		return err
	}
	for _, w := range t.Works {
		if _, err := ex.ExecContext(ctx, insertWorkQuery, w.ID, t.ID, w.WorkID, w.Name, w.Status); err != nil {
			return err
		}
	}
	return r.appendHistory(ctx, ex, t.ID, changes)
}

func (r *Postgres) Save(ctx context.Context, ex db.Executor, t *task.Task, changes []work.Change) error {
	if _, err := ex.ExecContext(ctx, updateTaskQuery, t.ID, t.Status, nullableTime(t.StartedAt), nullableTime(t.CompletedAt)); err != nil {
		return err
	}
	for _, w := range t.Works {
		if _, err := ex.ExecContext(ctx, updateWorkQuery, w.ID, w.Status); err != nil {
			return err
		}
	}
	return r.appendHistory(ctx, ex, t.ID, changes)
}

func (r *Postgres) appendHistory(ctx context.Context, ex db.Executor, taskID string, changes []work.Change) error {
	for _, ch := range changes {
		var prev sql.NullString
		if ch.Previous != nil {
			prev = sql.NullString{String: string(*ch.Previous), Valid: true}
		}
		if _, err := ex.ExecContext(ctx, insertHistoryQry, r.ids.NewULID(), taskID, ch.WorkID, prev, ch.New); err != nil {
			return err
		}
	}
	return nil
}

// worksByTask loads the works of the given tasks, grouped by task id.
func (r *Postgres) worksByTask(ctx context.Context, ex db.Executor, taskIDs []string) (map[string][]*work.Work, error) {
	rows, err := ex.QueryContext(ctx, selectWorksByTaskQuery, pq.Array(taskIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string][]*work.Work, len(taskIDs))
	for rows.Next() {
		var w work.Work
		if err := rows.Scan(&w.ID, &w.TaskID, &w.WorkID, &w.Name, &w.Status); err != nil {
			return nil, err
		}
		out[w.TaskID] = append(out[w.TaskID], &w)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(s rowScanner) (*task.Task, error) {
	var (
		t                  task.Task
		started, completed sql.NullTime
		createdAt          time.Time
	)
	if err := s.Scan(&t.ID, &t.ServiceOrderID, &t.SagaID, &t.Status, &createdAt, &started, &completed); err != nil {
		return nil, err
	}
	t.CreatedAt = createdAt
	if started.Valid {
		v := started.Time
		t.StartedAt = &v
	}
	if completed.Valid {
		v := completed.Time
		t.CompletedAt = &v
	}
	return &t, nil
}

func nullableTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
