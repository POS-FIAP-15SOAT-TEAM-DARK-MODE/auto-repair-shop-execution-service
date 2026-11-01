package task

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

// Controller exposes the execution REST API. Route params are read from the context
// (routing.GinHandler copies the gin params there).
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller {
	return &Controller{svc: svc}
}

type queueResponse struct {
	Items []queueItem `json:"items"`
}

type queueItem struct {
	TaskID         string      `json:"taskId"`
	ServiceOrderID string      `json:"serviceOrderId"`
	Status         string      `json:"status"`
	CreatedAt      time.Time   `json:"createdAt"`
	Works          []queueWork `json:"works"`
}

type queueWork struct {
	WorkID string `json:"workId"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Queue lists the QUEUED and IN_PROGRESS tasks, oldest first.
func (c *Controller) Queue(ctx context.Context, _ *http.Request) (queueResponse, error) {
	tasks, err := c.svc.Queue(ctx)
	if err != nil {
		return queueResponse{}, err
	}
	items := make([]queueItem, 0, len(tasks))
	for _, t := range tasks {
		works := make([]queueWork, 0, len(t.Works))
		for _, w := range t.Works {
			works = append(works, queueWork{WorkID: w.WorkID, Name: w.Name, Status: string(w.Status)})
		}
		items = append(items, queueItem{
			TaskID:         t.ID,
			ServiceOrderID: t.ServiceOrderID,
			Status:         string(t.Status),
			CreatedAt:      t.CreatedAt,
			Works:          works,
		})
	}
	return queueResponse{Items: items}, nil
}

// NextWork moves a work of the service order to its next status.
func (c *Controller) NextWork(ctx context.Context, req *http.Request) error {
	return c.svc.Next(ctx, paramFrom(ctx, "id"), paramFrom(ctx, "workId"), requestID(req))
}

// CancelWork cancels a work of the service order.
func (c *Controller) CancelWork(ctx context.Context, req *http.Request) error {
	return c.svc.CancelWork(ctx, paramFrom(ctx, "id"), paramFrom(ctx, "workId"), requestID(req))
}

// Finish closes the task of the service order.
func (c *Controller) Finish(ctx context.Context, req *http.Request) error {
	return c.svc.Finish(ctx, paramFrom(ctx, "id"), requestID(req))
}

func paramFrom(ctx context.Context, key string) string {
	v, _ := ctx.Value(key).(string)
	return v
}

func requestID(req *http.Request) string {
	if id := req.Header.Get(requestIDHeader); id != "" {
		return id
	}
	return uuid.NewString()
}
