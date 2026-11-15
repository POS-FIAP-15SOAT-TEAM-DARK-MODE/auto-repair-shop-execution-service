package container

import (
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/gin-gonic/gin"
)

// HandlersWrapper groups the HTTP controllers of the service.
type HandlersWrapper struct {
	Task *task.Controller
}

type Middlewares map[string]gin.HandlerFunc
