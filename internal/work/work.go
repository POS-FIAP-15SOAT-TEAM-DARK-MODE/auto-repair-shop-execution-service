// Package work holds the execution status of one work inside a task.
// The state machine is the same used by the monolith (service_order/domain/work_service_order_history.go).
package work

import "errors"

type Status string

const (
	StatusAwaitingStart Status = "AWAITING_START"
	StatusInProgress    Status = "IN_PROGRESS"
	StatusCompleted     Status = "COMPLETED"
	StatusCancelled     Status = "CANCELLED"
)

// statusOrder is the forward path of a work. CANCELLED is outside of it (terminal).
var statusOrder = []Status{StatusAwaitingStart, StatusInProgress, StatusCompleted}

var (
	ErrWorkServiceOrderNotFound         = errors.New("work has no history in this service order")
	ErrWorkServiceOrderAlreadyCompleted = errors.New("work has already been completed")
	ErrWorkServiceOrderAlreadyCancelled = errors.New("work has already been cancelled")
	ErrWorkServiceOrderInvalidStatus    = errors.New("invalid work status transition")
)

// Work is one work of a task. ID is the execution_work id; WorkID is the catalog work id.
type Work struct {
	ID     string
	TaskID string
	WorkID string
	Name   string
	Status Status
}

// Change is one status transition of a work. Previous is nil on creation.
type Change struct {
	WorkID   string
	Name     string
	Previous *Status
	New      Status
}

// IsTerminal reports whether the status is COMPLETED or CANCELLED.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// NextStatus returns the status that follows the current one on the forward path.
func (w *Work) NextStatus() (Status, error) {
	if w.Status == StatusCancelled {
		return "", ErrWorkServiceOrderAlreadyCancelled
	}
	for i, s := range statusOrder {
		if s == w.Status {
			if i+1 >= len(statusOrder) {
				return "", ErrWorkServiceOrderAlreadyCompleted
			}
			return statusOrder[i+1], nil
		}
	}
	return "", ErrWorkServiceOrderInvalidStatus
}

// Advance moves the work to NextStatus and returns the change.
func (w *Work) Advance() (Change, error) {
	next, err := w.NextStatus()
	if err != nil {
		return Change{}, err
	}
	return w.move(next), nil
}

// Cancel moves an AWAITING_START or IN_PROGRESS work to CANCELLED. Other statuses are rejected.
func (w *Work) Cancel() (Change, error) {
	switch w.Status {
	case StatusAwaitingStart, StatusInProgress:
		return w.move(StatusCancelled), nil
	case StatusCompleted:
		return Change{}, ErrWorkServiceOrderAlreadyCompleted
	case StatusCancelled:
		return Change{}, ErrWorkServiceOrderAlreadyCancelled
	default:
		return Change{}, ErrWorkServiceOrderInvalidStatus
	}
}

func (w *Work) move(to Status) Change {
	prev := w.Status
	w.Status = to
	return Change{WorkID: w.WorkID, Name: w.Name, Previous: &prev, New: to}
}
