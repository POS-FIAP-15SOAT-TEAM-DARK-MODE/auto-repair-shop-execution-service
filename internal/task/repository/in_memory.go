package repository

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/db"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
)

// HistoryEntry is one row of execution_work_history kept by the in-memory repository.
type HistoryEntry struct {
	TaskID   string
	WorkID   string
	Previous *work.Status
	New      work.Status
}

// InMemory implements task.Repository without a database. It is meant for unit tests.
type InMemory struct {
	mu      sync.Mutex
	tasks   map[string]task.Task // by service order id
	History []HistoryEntry
}

func NewInMemory() *InMemory {
	return &InMemory{tasks: map[string]task.Task{}}
}

var errDuplicateTask = errors.New("task already exists for this service order")

func (m *InMemory) FindByServiceOrderID(_ context.Context, _ db.Executor, serviceOrderID string) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[serviceOrderID]
	if !ok {
		return nil, task.ErrTaskNotFound
	}
	return copyTask(t), nil
}

func (m *InMemory) ListActive(_ context.Context, _ db.Executor) ([]*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*task.Task
	for _, t := range m.tasks {
		if t.IsActive() {
			out = append(out, copyTask(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *InMemory) Insert(_ context.Context, _ db.Executor, t *task.Task, changes []work.Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[t.ServiceOrderID]; exists {
		return errDuplicateTask
	}
	m.tasks[t.ServiceOrderID] = *copyTask(*t)
	m.appendHistory(t.ID, changes)
	return nil
}

func (m *InMemory) Save(_ context.Context, _ db.Executor, t *task.Task, changes []work.Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[t.ServiceOrderID]; !exists {
		return task.ErrTaskNotFound
	}
	m.tasks[t.ServiceOrderID] = *copyTask(*t)
	m.appendHistory(t.ID, changes)
	return nil
}

func (m *InMemory) appendHistory(taskID string, changes []work.Change) {
	for _, ch := range changes {
		m.History = append(m.History, HistoryEntry{TaskID: taskID, WorkID: ch.WorkID, Previous: ch.Previous, New: ch.New})
	}
}

// copyTask returns a copy whose works are not shared with the original.
func copyTask(t task.Task) *task.Task {
	c := t
	c.Works = make([]*work.Work, 0, len(t.Works))
	for _, w := range t.Works {
		cw := *w
		c.Works = append(c.Works, &cw)
	}
	return &c
}
