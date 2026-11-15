package task_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routeCtx mimics routing.GinHandler, which copies the gin params into the request context.
func routeCtx(params map[string]string) context.Context {
	ctx := context.Background()
	for k, v := range params {
		ctx = context.WithValue(ctx, k, v) //nolint:staticcheck
	}
	return ctx
}

func workRoute() context.Context {
	return routeCtx(map[string]string{"id": soID, "workId": "w-1"})
}

func serviceOrderRoute() context.Context {
	return routeCtx(map[string]string{"id": soID})
}

func TestController_QueueEmptyReturnsEmptyItems(t *testing.T) {
	f := newFixture(t)
	c := task.NewController(f.svc)

	resp, err := c.Queue(context.Background(), httptest.NewRequest(http.MethodGet, "/v1/execution/queue", nil))
	require.NoError(t, err)
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[]}`, string(raw))
}

func TestController_QueueShapesResponse(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1", "w-2"}, start("w-1"))
	c := task.NewController(f.svc)

	resp, err := c.Queue(context.Background(), httptest.NewRequest(http.MethodGet, "/v1/execution/queue", nil))
	require.NoError(t, err)
	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var body struct {
		Items []struct {
			TaskID         string `json:"taskId"`
			ServiceOrderID string `json:"serviceOrderId"`
			Status         string `json:"status"`
			CreatedAt      string `json:"createdAt"`
			Works          []struct {
				WorkID string `json:"workId"`
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"works"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Items, 1)
	item := body.Items[0]
	assert.Equal(t, tk.ID, item.TaskID)
	assert.Equal(t, soID, item.ServiceOrderID)
	assert.Equal(t, "IN_PROGRESS", item.Status)
	assert.NotEmpty(t, item.CreatedAt)
	require.Len(t, item.Works, 2)
	assert.Equal(t, "w-1", item.Works[0].WorkID)
	assert.Equal(t, "name-w-1", item.Works[0].Name)
	assert.Equal(t, "IN_PROGRESS", item.Works[0].Status)
	assert.Equal(t, "AWAITING_START", item.Works[1].Status)
}

func TestController_NextWorkUsesRouteParamsAndRequestHeader(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1"})
	c := task.NewController(f.svc)

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "new": "IN_PROGRESS"}})
	req := httptest.NewRequest(http.MethodPut, "/", nil)
	req.Header.Set("X-Request-Id", "req-header")
	require.NoError(t, c.NextWork(workRoute(), req))
	require.NoError(t, f.mock.ExpectationsWereMet())
	assert.Equal(t, task.StatusInProgress, f.statusOf())
}

func TestController_NextWorkGeneratesRequestIDWhenMissing(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1"})
	c := task.NewController(f.svc)

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "new": "IN_PROGRESS"}})
	require.NoError(t, c.NextWork(workRoute(), httptest.NewRequest(http.MethodPut, "/", nil)))
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestController_NextWorkMissingTaskIsNotFound(t *testing.T) {
	f := newFixture(t)
	c := task.NewController(f.svc)

	f.expectRolledBack()
	err := c.NextWork(workRoute(), httptest.NewRequest(http.MethodPut, "/", nil))
	assert.ErrorIs(t, err, task.ErrTaskNotFound)
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestController_CancelWork(t *testing.T) {
	f := newFixture(t)
	f.seed([]string{"w-1"})
	c := task.NewController(f.svc)

	f.expectTx(event{typ: "ExecutionWorkStatusChanged", saga: taskSaga, payload: map[string]any{"work_id": "w-1", "new": "CANCELLED"}})
	require.NoError(t, c.CancelWork(workRoute(), httptest.NewRequest(http.MethodPut, "/", nil)))
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestController_Finish(t *testing.T) {
	f := newFixture(t)
	tk := f.seed([]string{"w-1"}, complete("w-1"))
	c := task.NewController(f.svc)

	f.expectTx(event{typ: "ExecutionCompleted", saga: taskSaga, payload: map[string]any{"task_id": tk.ID}})
	require.NoError(t, c.Finish(serviceOrderRoute(), httptest.NewRequest(http.MethodPut, "/", nil)))
	require.NoError(t, f.mock.ExpectationsWereMet())
}
