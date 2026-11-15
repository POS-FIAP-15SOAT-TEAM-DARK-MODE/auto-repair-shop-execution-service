package task

import (
	"context"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/db"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
)

// Repository stores tasks, their works and the work history. Methods run on the given executor,
// which is a *sql.Tx inside command handlers and REST mutations.
type Repository interface {
	// FindByServiceOrderID returns the task of the service order (any status) or ErrTaskNotFound.
	FindByServiceOrderID(ctx context.Context, ex db.Executor, serviceOrderID string) (*Task, error)
	// ListActive returns QUEUED and IN_PROGRESS tasks, oldest first, with their works.
	ListActive(ctx context.Context, ex db.Executor) ([]*Task, error)
	// Insert stores a new task, its works and one history row per change.
	Insert(ctx context.Context, ex db.Executor, t *Task, changes []work.Change) error
	// Save updates the task and its works, and appends one history row per change.
	Save(ctx context.Context, ex db.Executor, t *Task, changes []work.Change) error
}
