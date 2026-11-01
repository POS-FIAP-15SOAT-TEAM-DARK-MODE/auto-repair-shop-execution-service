package routing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const soID = "01JD8Z4R3Q8M2V7K5N9XWABCDE"

type env struct {
	h    http.Handler
	mem  *repository.InMemory
	mock sqlmock.Sqlmock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	mem := repository.NewInMemory()
	svc := task.NewService(database, mem, func() string { return "id" })
	router := routing.SetupRouter(&container.HandlersWrapper{Task: task.NewController(svc)}, &container.Middlewares{})
	return &env{h: router, mem: mem, mock: mock}
}

// seed stores a QUEUED task for soID with one work "w-1".
func (e *env) seed(t *testing.T) {
	t.Helper()
	tk, err := task.New(func() string { return "id" }, soID, "saga-1", time.Now().UTC(), []task.NewWork{{WorkID: "w-1", Name: "Troca"}})
	require.NoError(t, err)
	require.NoError(t, e.mem.Insert(context.Background(), nil, tk, tk.TakeChanges()))
}

// expectRollback registers a transaction that is opened and rolled back (lookup failed).
func (e *env) expectRollback() {
	e.mock.ExpectBegin()
	e.mock.ExpectRollback()
}

// expectCommit registers a transaction that publishes n outbox events and commits.
func (e *env) expectCommit(n int) {
	e.mock.ExpectBegin()
	for i := 0; i < n; i++ {
		e.mock.ExpectExec("INSERT INTO outbox").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	e.mock.ExpectCommit()
}

func tokenFor(t *testing.T, roles ...auth.Role) string {
	t.Helper()
	tok, err := auth.GenerateToken("user-1", roles, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return "Bearer " + tok
}

func do(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes_QueueRoles(t *testing.T) {
	e := newEnv(t)
	const path = "/v1/execution/queue"

	assert.Equal(t, http.StatusUnauthorized, do(e.h, http.MethodGet, path, "").Code)
	assert.Equal(t, http.StatusForbidden, do(e.h, http.MethodGet, path, tokenFor(t, auth.CUSTOMER)).Code)
	assert.Equal(t, http.StatusOK, do(e.h, http.MethodGet, path, tokenFor(t, auth.ATTENDANT)).Code)
	assert.Equal(t, http.StatusOK, do(e.h, http.MethodGet, path, tokenFor(t, auth.MECHANIC)).Code)
	assert.Equal(t, http.StatusOK, do(e.h, http.MethodGet, path, tokenFor(t, auth.ADMIN)).Code)
}

func TestRoutes_QueueListsTasksWithWorks(t *testing.T) {
	e := newEnv(t)
	e.seed(t)

	rec := do(e.h, http.MethodGet, "/v1/execution/queue", tokenFor(t, auth.ATTENDANT))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"serviceOrderId":"`+soID+`"`)
	assert.Contains(t, rec.Body.String(), `"workId":"w-1"`)
}

func TestRoutes_MechanicOperationsRoles(t *testing.T) {
	paths := map[string]string{
		"next":   "/v1/service-order/" + soID + "/work/w-1/next",
		"cancel": "/v1/service-order/" + soID + "/work/w-1/cancel",
		"finish": "/v1/service-order/" + soID + "/finish",
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			assert.Equal(t, http.StatusUnauthorized, do(e.h, http.MethodPut, path, "").Code)
			assert.Equal(t, http.StatusForbidden, do(e.h, http.MethodPut, path, tokenFor(t, auth.ATTENDANT)).Code)
			assert.Equal(t, http.StatusForbidden, do(e.h, http.MethodPut, path, tokenFor(t, auth.CUSTOMER)).Code)

			// Allowed roles reach the controller; with no task for the service order the answer is 404.
			e.expectRollback()
			e.expectRollback()
			assert.Equal(t, http.StatusNotFound, do(e.h, http.MethodPut, path, tokenFor(t, auth.MECHANIC)).Code)
			assert.Equal(t, http.StatusNotFound, do(e.h, http.MethodPut, path, tokenFor(t, auth.ADMIN)).Code)
			require.NoError(t, e.mock.ExpectationsWereMet())
		})
	}
}

func TestRoutes_NextThenFinishCompletesTask(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	mech := tokenFor(t, auth.MECHANIC)

	e.expectCommit(1) // AWAITING_START -> IN_PROGRESS
	assert.Equal(t, http.StatusNoContent, do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/work/w-1/next", mech).Code)

	e.expectRollback() // finish while the work is still in progress
	assert.Equal(t, http.StatusConflict, do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/finish", mech).Code)

	e.expectCommit(1) // IN_PROGRESS -> COMPLETED
	assert.Equal(t, http.StatusNoContent, do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/work/w-1/next", mech).Code)

	e.expectCommit(1) // ExecutionCompleted
	assert.Equal(t, http.StatusNoContent, do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/finish", mech).Code)
	require.NoError(t, e.mock.ExpectationsWereMet())
}

func TestRoutes_CancelWorkRejectsCompletedWork(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	mech := tokenFor(t, auth.MECHANIC)

	e.expectCommit(1)
	do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/work/w-1/next", mech)
	e.expectCommit(1)
	do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/work/w-1/next", mech)

	e.expectRollback()
	rec := do(e.h, http.MethodPut, "/v1/service-order/"+soID+"/work/w-1/cancel", mech)
	assert.Equal(t, http.StatusConflict, rec.Code)
	require.NoError(t, e.mock.ExpectationsWereMet())
}
