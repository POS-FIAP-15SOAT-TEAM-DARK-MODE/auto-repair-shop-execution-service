package web

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/json"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
	"github.com/stretchr/testify/assert"
)

func TestError_StatusCodes(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
	}{
		"work not found":         {work.ErrWorkServiceOrderNotFound, http.StatusNotFound},
		"task not found":         {task.ErrTaskNotFound, http.StatusNotFound},
		"task not active":        {task.ErrTaskNotActive, http.StatusConflict},
		"works not finished":     {task.ErrWorksNotFinished, http.StatusConflict},
		"work already completed": {work.ErrWorkServiceOrderAlreadyCompleted, http.StatusConflict},
		"work already cancelled": {work.ErrWorkServiceOrderAlreadyCancelled, http.StatusConflict},
		"work invalid status":    {work.ErrWorkServiceOrderInvalidStatus, http.StatusConflict},
		"data conflict":          {app.ErrDataConflict, http.StatusConflict},
		"infra conflict":         {app.ErrInfraConflict, http.StatusConflict},
		"data violation":         {app.ErrDataViolation, http.StatusUnprocessableEntity},
		"json syntax":            {jsonv2.ErrJSONSyntax, http.StatusBadRequest},
		"json empty body":        {jsonv2.ErrJSONEmptyBody, http.StatusBadRequest},
		"json unsupported value": {jsonv2.ErrJSONUnsupportedValue, http.StatusInternalServerError},
		"joined errors":          {errors.Join(errors.New("a"), errors.New("b")), http.StatusBadRequest},
		"unknown error":          {errors.New("unexpected"), http.StatusInternalServerError},
		"wrapped not found":      {fmt.Errorf("lookup: %w", task.ErrTaskNotFound), http.StatusNotFound},
		"wrapped conflict":       {fmt.Errorf("finish: %w", task.ErrWorksNotFinished), http.StatusConflict},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, _ := Error(tc.err)
			assert.Equal(t, tc.status, status)
		})
	}
}

func TestError_BodyShape(t *testing.T) {
	status, body := Error(task.ErrTaskNotActive)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, http.StatusConflict, body.StatusCode)
	assert.Equal(t, task.ErrTaskNotActive.Error(), body.Error)
	assert.Nil(t, body.Errors)

	status, body = Error(task.ErrTaskNotFound)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, task.ErrTaskNotFound.Error(), body.Error)

	status, body = Error(errors.Join(errors.New("first"), errors.New("second")))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{"first", "second"}, body.Errors)
	assert.Empty(t, body.Error)

	// Same rule as the monolith: 5xx shows the innermost message.
	status, body = Error(fmt.Errorf("outer: %w", errors.New("root cause")))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "root cause", body.Error)
}

func TestError_RootCauseForConflict(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", work.ErrWorkServiceOrderAlreadyCancelled))
	_, body := Error(wrapped)
	assert.Equal(t, work.ErrWorkServiceOrderAlreadyCancelled.Error(), body.Error)
}
