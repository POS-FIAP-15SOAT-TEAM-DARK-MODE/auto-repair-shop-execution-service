// Package task is the execution aggregate: a task per service order, with its works.
package task

import (
	"errors"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
)

type Status string

const (
	StatusQueued     Status = "QUEUED"
	StatusInProgress Status = "IN_PROGRESS"
	StatusCompleted  Status = "COMPLETED"
	StatusCancelled  Status = "CANCELLED"
)

var (
	ErrTaskNotFound     = errors.New("execution task not found for this service order")
	ErrTaskNotActive    = errors.New("execution task is not queued or in progress")
	ErrWorksNotFinished = errors.New("all works must be completed or cancelled before finishing")
	ErrInvalidWorks     = errors.New("invalid works: at least one work is required and work_id must be unique and non-empty")
)

// Active reports whether the task can still receive work updates.
func (s Status) Active() bool {
	return s == StatusQueued || s == StatusInProgress
}

// FinishOutcome is the result of Finish: which event must follow the REST call.
type FinishOutcome string

const (
	FinishCompleted    FinishOutcome = "COMPLETED"
	FinishAllCancelled FinishOutcome = "ALL_WORKS_CANCELLED"
)

// Task groups the works of one service order. SagaID is the saga of the EnqueueExecution that created it.
type Task struct {
	ID             string
	ServiceOrderID string
	SagaID         string
	Status         Status
	CreatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	Works          []*work.Work

	changes []work.Change
}

// NewWork is a work requested by EnqueueExecution.
type NewWork struct {
	WorkID string
	Name   string
}

// New builds a QUEUED task with AWAITING_START works. Empty or duplicated work ids are rejected.
func New(newID func() string, serviceOrderID, sagaID string, now time.Time, requested []NewWork) (*Task, error) {
	if len(requested) == 0 {
		return nil, ErrInvalidWorks
	}
	seen := make(map[string]struct{}, len(requested))
	for _, r := range requested {
		if r.WorkID == "" {
			return nil, ErrInvalidWorks
		}
		if _, dup := seen[r.WorkID]; dup {
			return nil, ErrInvalidWorks
		}
		seen[r.WorkID] = struct{}{}
	}

	t := &Task{
		ID:             newID(),
		ServiceOrderID: serviceOrderID,
		SagaID:         sagaID,
		Status:         StatusQueued,
		CreatedAt:      now,
	}
	for _, r := range requested {
		w := &work.Work{ID: newID(), TaskID: t.ID, WorkID: r.WorkID, Name: r.Name, Status: work.StatusAwaitingStart}
		t.Works = append(t.Works, w)
		t.changes = append(t.changes, work.Change{WorkID: r.WorkID, Name: r.Name, New: work.StatusAwaitingStart})
	}
	return t, nil
}

// IsActive reports whether the task is QUEUED or IN_PROGRESS.
func (t *Task) IsActive() bool {
	return t.Status.Active()
}

// Advance moves one work to its next status. The first work that starts moves the task to IN_PROGRESS.
func (t *Task) Advance(workID string, now time.Time) error {
	if !t.IsActive() {
		return ErrTaskNotActive
	}
	w, err := t.findWork(workID)
	if err != nil {
		return err
	}
	ch, err := w.Advance()
	if err != nil {
		return err
	}
	t.record(ch)
	if ch.New == work.StatusInProgress && t.Status == StatusQueued {
		t.Status = StatusInProgress
		t.StartedAt = &now
	}
	return nil
}

// CancelWork cancels one work that has not started or is in progress.
func (t *Task) CancelWork(workID string) error {
	if !t.IsActive() {
		return ErrTaskNotActive
	}
	w, err := t.findWork(workID)
	if err != nil {
		return err
	}
	ch, err := w.Cancel()
	if err != nil {
		return err
	}
	t.record(ch)
	return nil
}

// CancelAll cancels every non-terminal work and then the task (CancelExecution from the saga).
func (t *Task) CancelAll() error {
	if !t.IsActive() {
		return ErrTaskNotActive
	}
	for _, w := range t.Works {
		if w.Status.IsTerminal() {
			continue
		}
		ch, err := w.Cancel()
		if err != nil {
			return err
		}
		t.record(ch)
	}
	t.Status = StatusCancelled
	return nil
}

// Finish closes the task when every work is COMPLETED or CANCELLED.
// If all works were cancelled the task is CANCELLED, otherwise COMPLETED.
func (t *Task) Finish(now time.Time) (FinishOutcome, error) {
	if !t.IsActive() {
		return "", ErrTaskNotActive
	}
	allCancelled := true
	for _, w := range t.Works {
		if !w.Status.IsTerminal() {
			return "", ErrWorksNotFinished
		}
		if w.Status != work.StatusCancelled {
			allCancelled = false
		}
	}
	if allCancelled {
		t.Status = StatusCancelled
		return FinishAllCancelled, nil
	}
	t.Status = StatusCompleted
	t.CompletedAt = &now
	return FinishCompleted, nil
}

// TakeChanges returns the work transitions made since the last call and clears them.
func (t *Task) TakeChanges() []work.Change {
	c := t.changes
	t.changes = nil
	return c
}

func (t *Task) record(ch work.Change) {
	t.changes = append(t.changes, ch)
}

func (t *Task) findWork(workID string) (*work.Work, error) {
	for _, w := range t.Works {
		if w.WorkID == workID {
			return w, nil
		}
	}
	return nil, work.ErrWorkServiceOrderNotFound
}
