package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"go.uber.org/zap"
)

// Service applies the EnqueueExecution/CancelExecution commands and the REST work operations.
// Every state change, its history rows and its outbox events are written in one transaction.
type Service struct {
	db    *sql.DB
	repo  Repository
	newID func() string
}

func NewService(database *sql.DB, repo Repository, newID func() string) *Service {
	return &Service{db: database, repo: repo, newID: newID}
}

// RegisterHandlers binds the saga commands to the consumer.
func (s *Service) RegisterHandlers(c *messaging.Consumer) {
	c.RegisterHandler(cmdEnqueueExecution, s.EnqueueExecution)
	c.RegisterHandler(cmdCancelExecution, s.CancelExecution)
}

// EnqueueExecution creates the task of the service order. An invalid request is answered with
// ExecutionFailed (INVALID_REQUEST) and no task is created. A task that already exists is only logged
// when it is active, and answered with ExecutionFailed when it is already terminal.
func (s *Service) EnqueueExecution(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	var p enqueuePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return s.publishFailed(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, "", reasonInvalidRequest)
	}

	existing, err := s.repo.FindByServiceOrderID(ctx, tx, env.ServiceOrderID)
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return s.createTask(ctx, tx, env, p)
	case err != nil:
		return err
	case existing.IsActive():
		logger.Of(ctx).Info("execution.enqueue_ignored", zap.String("reason", "already_active"))
		return nil
	default:
		return s.publishFailed(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, existing.ID, reasonInvalidRequest)
	}
}

func (s *Service) createTask(ctx context.Context, tx *sql.Tx, env messaging.Envelope, p enqueuePayload) error {
	requested := make([]NewWork, 0, len(p.Works))
	for _, w := range p.Works {
		requested = append(requested, NewWork(w))
	}
	t, err := New(s.newID, env.ServiceOrderID, env.SagaID, time.Now().UTC(), requested)
	if errors.Is(err, ErrInvalidWorks) {
		logger.Of(ctx).Warn("execution.enqueue_rejected", zap.String("reason", reasonInvalidRequest))
		return s.publishFailed(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, "", reasonInvalidRequest)
	}
	if err != nil {
		return err
	}

	changes := t.TakeChanges()
	if err := s.repo.Insert(ctx, tx, t, changes); err != nil {
		return err
	}
	if err := s.publishChanges(ctx, tx, t.SagaID, t.ServiceOrderID, env.RequestID, changes); err != nil {
		return err
	}
	return s.publish(ctx, tx, t.SagaID, t.ServiceOrderID, env.RequestID, evExecutionQueued, taskPayload{TaskID: t.ID})
}

// CancelExecution cancels the active task of the service order (its non-terminal works and the task).
// Without an active task it only logs. Work changes are published; no ExecutionFailed is sent.
func (s *Service) CancelExecution(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	t, err := s.repo.FindByServiceOrderID(ctx, tx, env.ServiceOrderID)
	if errors.Is(err, ErrTaskNotFound) {
		logger.Of(ctx).Info("execution.cancel_ignored", zap.String("reason", "no_task"))
		return nil
	}
	if err != nil {
		return err
	}
	if !t.IsActive() {
		logger.Of(ctx).Info("execution.cancel_ignored", zap.String("reason", "task_finished"))
		return nil
	}

	if err := t.CancelAll(); err != nil {
		return err
	}
	changes := t.TakeChanges()
	if err := s.repo.Save(ctx, tx, t, changes); err != nil {
		return err
	}
	return s.publishChanges(ctx, tx, t.SagaID, t.ServiceOrderID, env.RequestID, changes)
}

// Queue returns the active tasks, oldest first.
func (s *Service) Queue(ctx context.Context) ([]*Task, error) {
	return s.repo.ListActive(ctx, s.db)
}

// Next advances one work of the service order's task.
func (s *Service) Next(ctx context.Context, serviceOrderID, workID, requestID string) error {
	return s.mutate(ctx, serviceOrderID, requestID, func(t *Task) error {
		return t.Advance(workID, time.Now().UTC())
	})
}

// CancelWork cancels one work of the service order's task.
func (s *Service) CancelWork(ctx context.Context, serviceOrderID, workID, requestID string) error {
	return s.mutate(ctx, serviceOrderID, requestID, func(t *Task) error {
		return t.CancelWork(workID)
	})
}

// Finish closes the service order's task once every work is COMPLETED or CANCELLED.
func (s *Service) Finish(ctx context.Context, serviceOrderID, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	t, err := s.repo.FindByServiceOrderID(ctx, tx, serviceOrderID)
	if err != nil {
		return err
	}
	outcome, err := t.Finish(time.Now().UTC())
	if err != nil {
		return err
	}
	if err := s.repo.Save(ctx, tx, t, nil); err != nil {
		return err
	}

	switch outcome {
	case FinishCompleted:
		err = s.publish(ctx, tx, t.SagaID, serviceOrderID, requestID, evExecutionCompleted, taskPayload{TaskID: t.ID})
	case FinishAllCancelled:
		err = s.publish(ctx, tx, t.SagaID, serviceOrderID, requestID, evExecutionFailed,
			executionFailedPayload{TaskID: t.ID, Reason: reasonAllWorksCancelled})
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// mutate loads the service order's task, applies op and persists the result with its events.
func (s *Service) mutate(ctx context.Context, serviceOrderID, requestID string, op func(*Task) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	t, err := s.repo.FindByServiceOrderID(ctx, tx, serviceOrderID)
	if err != nil {
		return err
	}
	if err := op(t); err != nil {
		return err
	}
	changes := t.TakeChanges()
	if err := s.repo.Save(ctx, tx, t, changes); err != nil {
		return err
	}
	if err := s.publishChanges(ctx, tx, t.SagaID, serviceOrderID, requestID, changes); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) publishChanges(ctx context.Context, tx *sql.Tx, sagaID, serviceOrderID, requestID string, changes []work.Change) error {
	for _, ch := range changes {
		payload := workStatusChangedPayload{WorkID: ch.WorkID, WorkName: ch.Name, New: string(ch.New)}
		if ch.Previous != nil {
			prev := string(*ch.Previous)
			payload.Previous = &prev
		}
		if err := s.publish(ctx, tx, sagaID, serviceOrderID, requestID, evExecutionWorkStatusChanged, payload); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) publishFailed(ctx context.Context, tx *sql.Tx, sagaID, serviceOrderID, requestID, taskID, reason string) error {
	return s.publish(ctx, tx, sagaID, serviceOrderID, requestID, evExecutionFailed, executionFailedPayload{TaskID: taskID, Reason: reason})
}

// publish enqueues an event carrying the saga, service order and request ids, and logs it
// with them so the saga can be followed across services by saga_id.
func (s *Service) publish(ctx context.Context, tx *sql.Tx, sagaID, serviceOrderID, requestID, typ string, payload any) error {
	ev, err := messaging.NewEnvelope(typ, sagaID, serviceOrderID, requestID, payload)
	if err != nil {
		return fmt.Errorf("build %s: %w", typ, err)
	}
	if err := messaging.Enqueue(ctx, tx, topicExecutionEvents, "", ev); err != nil {
		return err
	}
	logger.Of(ctx).Info("outbox.enqueued",
		zap.String("type", typ),
		zap.String("saga_id", sagaID),
		zap.String("service_order_id", serviceOrderID),
		zap.String("message_id", ev.MessageID),
	)
	return nil
}
