package task_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/db"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	soID      = "01JD8Z4R3Q8M2V7K5N9XWABCDE"
	cmdSaga   = "saga-command"
	taskSaga  = "saga-task"
	requestID = "req-1"

	enqueueTwoWorks = `{"works":[{"work_id":"w-1","name":"Troca de óleo"},{"work_id":"w-2","name":"Filtro"}]}`
)

var errBoom = errors.New("boom")

// failingRepo returns the configured errors and delegates everything else to the in-memory repository.
type failingRepo struct {
	*repository.InMemory
	findErr, insertErr, saveErr error
}

func (r failingRepo) FindByServiceOrderID(ctx context.Context, ex db.Executor, id string) (*task.Task, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.InMemory.FindByServiceOrderID(ctx, ex, id)
}

func (r failingRepo) Insert(ctx context.Context, ex db.Executor, t *task.Task, ch []work.Change) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	return r.InMemory.Insert(ctx, ex, t, ch)
}

func (r failingRepo) Save(ctx context.Context, ex db.Executor, t *task.Task, ch []work.Change) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.InMemory.Save(ctx, ex, t, ch)
}

type fixture struct {
	t    *testing.T
	db   *sql.DB
	mock sqlmock.Sqlmock
	repo task.Repository
	mem  *repository.InMemory
	svc  *task.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return buildFixture(t, nil)
}

// buildFixture wires the service with a repository wrapper (nil means the in-memory repository).
func buildFixture(t *testing.T, wrap func(*repository.InMemory) task.Repository) *fixture {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	mem := repository.NewInMemory()
	var repo task.Repository = mem
	if wrap != nil {
		repo = wrap(mem)
	}
	return &fixture{t: t, db: database, mock: mock, repo: repo, mem: mem, svc: task.NewService(database, repo, counterID())}
}

func counterID() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("id-%d", n)
	}
}

// event expects one outbox insert with the envelope type, saga and payload fields.
type event struct {
	typ     string
	saga    string
	payload map[string]any
}

func (e event) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	var env struct {
		Type    string         `json:"type"`
		SagaID  string         `json:"saga_id"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return false
	}
	if env.Type != e.typ {
		return false
	}
	if e.saga != "" && env.SagaID != e.saga {
		return false
	}
	for k, want := range e.payload {
		if !reflect.DeepEqual(env.Payload[k], want) {
			return false
		}
	}
	return true
}

func (f *fixture) expectEvents(events ...event) {
	f.t.Helper()
	for _, e := range events {
		f.mock.ExpectExec("INSERT INTO outbox").
			WithArgs(sqlmock.AnyArg(), "execution-events", nil, e.typ, e).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
}

// expectTx registers a transaction that publishes events and commits.
func (f *fixture) expectTx(events ...event) {
	f.mock.ExpectBegin()
	f.expectEvents(events...)
	f.mock.ExpectCommit()
}

// expectRolledBack registers a transaction that fails and rolls back.
func (f *fixture) expectRolledBack() {
	f.mock.ExpectBegin()
	f.mock.ExpectRollback()
}

// beginTx starts a transaction on the mock database, as the consumer does before a handler.
func (f *fixture) beginTx() *sql.Tx {
	f.t.Helper()
	f.mock.ExpectBegin()
	tx, err := f.db.Begin()
	require.NoError(f.t, err)
	return tx
}

func envelope(typ, payload string) messaging.Envelope {
	return messaging.Envelope{
		MessageID:      "b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11",
		SagaID:         cmdSaga,
		Type:           typ,
		ServiceOrderID: soID,
		RequestID:      requestID,
		Payload:        json.RawMessage(payload),
	}
}

// step changes the task while it is seeded (domain operations, no outbox).
type step func(tk *task.Task)

func start(workID string) step {
	return func(tk *task.Task) { must(tk.Advance(workID, time.Now())) }
}

func complete(workID string) step {
	return func(tk *task.Task) {
		must(tk.Advance(workID, time.Now()))
		must(tk.Advance(workID, time.Now()))
	}
}

func cancel(workID string) step {
	return func(tk *task.Task) { must(tk.CancelWork(workID)) }
}

func finish() step {
	return func(tk *task.Task) {
		_, err := tk.Finish(time.Now())
		must(err)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// seed stores a task for soID with the given works and applies the steps, returning it.
func (f *fixture) seed(workIDs []string, steps ...step) *task.Task {
	f.t.Helper()
	req := make([]task.NewWork, 0, len(workIDs))
	for _, w := range workIDs {
		req = append(req, task.NewWork{WorkID: w, Name: "name-" + w})
	}
	tk, err := task.New(counterID(), soID, taskSaga, time.Now().UTC(), req)
	require.NoError(f.t, err)
	for _, s := range steps {
		s(tk)
	}
	require.NoError(f.t, f.repo.Insert(context.Background(), nil, tk, tk.TakeChanges()))
	return tk
}

func (f *fixture) statusOf() task.Status {
	f.t.Helper()
	tk, err := f.repo.FindByServiceOrderID(context.Background(), nil, soID)
	require.NoError(f.t, err)
	return tk.Status
}

func TestEnqueueExecution_CreatesTaskAndPublishesQueued(t *testing.T) {
	f := newFixture(t)
	tx := f.beginTx()
	f.expectEvents(
		event{typ: "ExecutionWorkStatusChanged", saga: cmdSaga, payload: map[string]any{"work_id": "w-1", "work_name": "Troca de óleo", "previous": nil, "new": "AWAITING_START"}},
		event{typ: "ExecutionWorkStatusChanged", saga: cmdSaga, payload: map[string]any{"work_id": "w-2", "work_name": "Filtro", "previous": nil, "new": "AWAITING_START"}},
		event{typ: "ExecutionQueued", saga: cmdSaga, payload: map[string]any{"task_id": "id-1"}},
	)
	f.mock.ExpectCommit()

	require.NoError(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", enqueueTwoWorks)))
	require.NoError(t, tx.Commit())
	require.NoError(t, f.mock.ExpectationsWereMet())

	tk, err := f.repo.FindByServiceOrderID(context.Background(), nil, soID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusQueued, tk.Status)
	assert.Equal(t, cmdSaga, tk.SagaID, "the saga of the command is stored on the task")
	assert.Len(t, tk.Works, 2)
	assert.Len(t, f.mem.History, 2)
}

func TestEnqueueExecution_InvalidRequestAnswersFailedWithoutTask(t *testing.T) {
	cases := map[string]string{
		"malformed json":   `{"works":`,
		"no works":         `{"works":[]}`,
		"empty work id":    `{"works":[{"work_id":"","name":"x"}]}`,
		"repeated work id": `{"works":[{"work_id":"w-1","name":"a"},{"work_id":"w-1","name":"b"}]}`,
		"works not a list": `{"works":"w-1"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tx := f.beginTx()
			f.expectEvents(event{typ: "ExecutionFailed", saga: cmdSaga, payload: map[string]any{"task_id": "", "reason": "INVALID_REQUEST"}})
			f.mock.ExpectCommit()

			require.NoError(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", payload)))
			require.NoError(t, tx.Commit())
			require.NoError(t, f.mock.ExpectationsWereMet())
			_, err := f.repo.FindByServiceOrderID(context.Background(), nil, soID)
			assert.ErrorIs(t, err, task.ErrTaskNotFound)
		})
	}
}

func TestEnqueueExecution_ActiveTaskIsNotChanged(t *testing.T) {
	cases := map[string][]step{
		"queued":      nil,
		"in progress": {start("w-1")},
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.seed([]string{"w-1", "w-2"}, steps...)
			tx := f.beginTx()
			f.mock.ExpectCommit()

			require.NoError(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", enqueueTwoWorks)))
			require.NoError(t, tx.Commit())
			require.NoError(t, f.mock.ExpectationsWereMet(), "a duplicate must not publish anything")
			assert.True(t, f.statusOf().Active())
		})
	}
}

func TestEnqueueExecution_TerminalTaskAnswersInvalidRequest(t *testing.T) {
	cases := map[string]struct {
		steps  []step
		status task.Status
	}{
		"completed": {steps: []step{complete("w-1"), finish()}, status: task.StatusCompleted},
		"cancelled": {steps: []step{cancel("w-1"), finish()}, status: task.StatusCancelled},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			existing := f.seed([]string{"w-1"}, tc.steps...)
			tx := f.beginTx()
			f.expectEvents(event{typ: "ExecutionFailed", saga: cmdSaga, payload: map[string]any{"task_id": existing.ID, "reason": "INVALID_REQUEST"}})
			f.mock.ExpectCommit()

			require.NoError(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", enqueueTwoWorks)))
			require.NoError(t, tx.Commit())
			require.NoError(t, f.mock.ExpectationsWereMet())
			assert.Equal(t, tc.status, f.statusOf())
		})
	}
}

func TestEnqueueExecution_RepositoryErrorsPropagate(t *testing.T) {
	t.Run("find", func(t *testing.T) {
		f := buildFixture(t, func(m *repository.InMemory) task.Repository { return failingRepo{InMemory: m, findErr: errBoom} })
		err := f.svc.EnqueueExecution(context.Background(), f.beginTx(), envelope("EnqueueExecution", enqueueTwoWorks))
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("insert", func(t *testing.T) {
		f := buildFixture(t, func(m *repository.InMemory) task.Repository { return failingRepo{InMemory: m, insertErr: errBoom} })
		err := f.svc.EnqueueExecution(context.Background(), f.beginTx(), envelope("EnqueueExecution", enqueueTwoWorks))
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestEnqueueExecution_OutboxErrorPropagates(t *testing.T) {
	f := newFixture(t)
	tx := f.beginTx()
	f.mock.ExpectExec("INSERT INTO outbox").WillReturnError(errBoom)
	assert.ErrorIs(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", enqueueTwoWorks)), errBoom)
}

func TestCancelExecution_CancelsActiveTaskAndPublishesWorkChanges(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1", "w-2"}, start("w-1"))

	tx := f.beginTx()
	f.expectEvents(
		event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "previous": "IN_PROGRESS", "new": "CANCELLED"}},
		event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-2", "previous": "AWAITING_START", "new": "CANCELLED"}},
	)
	f.mock.ExpectCommit()

	require.NoError(t, f.svc.CancelExecution(context.Background(), tx, envelope("CancelExecution", "{}")))
	require.NoError(t, tx.Commit())
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusCancelled, f.statusOf())
}

func TestCancelExecution_WithoutActiveTaskOnlyCommits(t *testing.T) {
	cases := map[string]func(f *fixture){
		"no task":        func(f *fixture) {},
		"completed task": func(f *fixture) { f.seed([]string{"w-1"}, complete("w-1"), finish()) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			setup(f)
			tx := f.beginTx()
			f.mock.ExpectCommit()
			require.NoError(t, f.svc.CancelExecution(context.Background(), tx, envelope("CancelExecution", "{}")))
			require.NoError(t, tx.Commit())
			require.NoError(t, f.mock.ExpectationsWereMet())
		})
	}
}

func TestCancelExecution_RepositoryErrorsPropagate(t *testing.T) {
	f := buildFixture(t, func(m *repository.InMemory) task.Repository { return failingRepo{InMemory: m, findErr: errBoom} })
	assert.ErrorIs(t, f.svc.CancelExecution(context.Background(), f.beginTx(), envelope("CancelExecution", "{}")), errBoom)

	g := buildFixture(t, func(m *repository.InMemory) task.Repository { return failingRepo{InMemory: m, saveErr: errBoom} })
	g.seed([]string{"w-1"})
	assert.ErrorIs(t, g.svc.CancelExecution(context.Background(), g.beginTx(), envelope("CancelExecution", "{}")), errBoom)
}

func TestQueue_ReturnsOnlyActiveTasks(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1"}, complete("w-1"), finish())
	tasks, err := f.svc.Queue(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tasks, "a completed task is not in the queue")

	g := newFixture(t)
	g.seed([]string{"w-1", "w-2"}, start("w-1"))
	tasks, err = g.svc.Queue(context.Background())
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, task.StatusInProgress, tasks[0].Status)
}

func TestNext_StartsWorkAndMovesTaskToInProgress(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1", "w-2"})

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "work_name": "name-w-1", "previous": "AWAITING_START", "new": "IN_PROGRESS"}})
	require.NoError(t, f.svc.Next(context.Background(), soID, "w-1", requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusInProgress, f.statusOf())
	assert.Equal(t, tk.ID, mustTask(t, f).ID)
}

func TestNext_CompletesWorkAndKeepsTaskInProgress(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1", "w-2"}, start("w-1"))

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "previous": "IN_PROGRESS", "new": "COMPLETED"}})
	require.NoError(t, f.svc.Next(context.Background(), soID, "w-1", requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusInProgress, f.statusOf())
}

func TestNext_ErrorsRollBack(t *testing.T) {
	cases := map[string]struct {
		seed func(f *fixture)
		work string
		want error
	}{
		"no task": {
			seed: func(f *fixture) {},
			work: "w-1", want: task.ErrTaskNotFound,
		},
		"task not active": {
			seed: func(f *fixture) { f.seed([]string{"w-1"}, complete("w-1"), finish()) },
			work: "w-1", want: task.ErrTaskNotActive,
		},
		"work not in task": {
			seed: func(f *fixture) { f.seed([]string{"w-1"}) },
			work: "nope", want: work.ErrWorkServiceOrderNotFound,
		},
		"work cancelled": {
			seed: func(f *fixture) { f.seed([]string{"w-1", "w-2"}, cancel("w-1")) },
			work: "w-1", want: work.ErrWorkServiceOrderAlreadyCancelled,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.seed(f)
			f.expectRolledBack()
			assert.ErrorIs(t, f.svc.Next(context.Background(), soID, tc.work, requestID), tc.want)
			require.NoError(t, f.mock.ExpectationsWereMet())
		})
	}
}

func TestNext_CompletedWorkCannotAdvance(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1"}, complete("w-1"))

	f.expectRolledBack()
	assert.ErrorIs(t, f.svc.Next(context.Background(), soID, "w-1", requestID), work.ErrWorkServiceOrderAlreadyCompleted)
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestCancelWork_PublishesChangeAndKeepsTask(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1", "w-2"})

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-2", "previous": "AWAITING_START", "new": "CANCELLED"}})
	require.NoError(t, f.svc.CancelWork(context.Background(), soID, "w-2", requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusQueued, f.statusOf())
}

func TestCancelWork_ErrorsRollBack(t *testing.T) {
	f := newFixture(t)
	f.expectRolledBack()
	assert.ErrorIs(t, f.svc.CancelWork(context.Background(), soID, "w-1", requestID), task.ErrTaskNotFound)
	require.NoError(t, f.mock.ExpectationsWereMet())

	f.seed([]string{"w-1"})
	f.expectRolledBack()
	assert.ErrorIs(t, f.svc.CancelWork(context.Background(), soID, "missing", requestID), work.ErrWorkServiceOrderNotFound)
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestFinish_AllCompletedPublishesExecutionCompleted(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1", "w-2"}, complete("w-1"), complete("w-2"))

	f.expectTx(event{typ: "ExecutionCompleted", saga: taskSaga, payload: map[string]any{"task_id": tk.ID}})
	require.NoError(t, f.svc.Finish(context.Background(), soID, requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusCompleted, f.statusOf())
}

func TestFinish_MixOfCompletedAndCancelledPublishesCompleted(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1", "w-2"}, complete("w-1"), cancel("w-2"))

	f.expectTx(event{typ: "ExecutionCompleted", saga: taskSaga, payload: map[string]any{"task_id": tk.ID}})
	require.NoError(t, f.svc.Finish(context.Background(), soID, requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusCompleted, f.statusOf())
}

func TestFinish_AllCancelledPublishesExecutionFailed(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1", "w-2"}, cancel("w-1"), cancel("w-2"))

	f.expectTx(event{typ: "ExecutionFailed", saga: taskSaga, payload: map[string]any{"task_id": tk.ID, "reason": "ALL_WORKS_CANCELLED"}})
	require.NoError(t, f.svc.Finish(context.Background(), soID, requestID))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusCancelled, f.statusOf())
}

func TestFinish_ErrorsRollBack(t *testing.T) {
	cases := map[string]struct {
		seed func(f *fixture)
		want error
	}{
		"no task":        {seed: func(f *fixture) {}, want: task.ErrTaskNotFound},
		"works pending":  {seed: func(f *fixture) { f.seed([]string{"w-1", "w-2"}, complete("w-1")) }, want: task.ErrWorksNotFinished},
		"task completed": {seed: func(f *fixture) { f.seed([]string{"w-1"}, complete("w-1"), finish()) }, want: task.ErrTaskNotActive},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.seed(f)
			f.expectRolledBack()
			assert.ErrorIs(t, f.svc.Finish(context.Background(), soID, requestID), tc.want)
			require.NoError(t, f.mock.ExpectationsWereMet())
		})
	}
}

func TestFinish_SaveErrorRollsBack(t *testing.T) {
	f := buildFixture(t, func(m *repository.InMemory) task.Repository { return failingRepo{InMemory: m, saveErr: errBoom} })
	f.seed([]string{"w-1"}, complete("w-1"))

	f.expectRolledBack()
	assert.ErrorIs(t, f.svc.Finish(context.Background(), soID, requestID), errBoom)
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestSagaIsKeptAcrossRESTAndCommandEvents(t *testing.T) {
	// EnqueueExecution stores cmdSaga; the REST events of that task must carry it (not the REST request).
	f := newFixture(t)
	tx := f.beginTx()
	f.expectEvents(
		event{typ: "ExecutionWorkStatusChanged", saga: cmdSaga},
		event{typ: "ExecutionWorkStatusChanged", saga: cmdSaga},
		event{typ: "ExecutionQueued", saga: cmdSaga},
	)
	f.mock.ExpectCommit()
	require.NoError(t, f.svc.EnqueueExecution(context.Background(), tx, envelope("EnqueueExecution", enqueueTwoWorks)))
	require.NoError(t, tx.Commit())
	require.NoError(t, f.mock.ExpectationsWereMet())

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: cmdSaga})
	require.NoError(t, f.svc.Next(context.Background(), soID, "w-1", "another-request"))
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func mustTask(t *testing.T, f *fixture) *task.Task {
	t.Helper()
	tk, err := f.repo.FindByServiceOrderID(context.Background(), nil, soID)
	require.NoError(t, err)
	return tk
}
