package routing

import (
	"context"
	http2 "net/http"

	api "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/docs"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/web"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

func SetupRouter(c *container.HandlersWrapper, m *container.Middlewares) *gin.Engine {
	router := gin.New()

	for name, mid := range *m {
		logger.Global().Info("Starting new middleware", zap.String("name", name))
		router.Use(mid)
	}

	mountSwaggerUI(router)

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http2.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := router.Group("/v1")

	v1.GET("/execution/queue", middleware.Auth(auth.AttendantAndMechanicRoles...), GinHandler(c.Task.Queue))
	v1.PUT("/service-order/:id/work/:workId/next", middleware.Auth(auth.MechanicRoles...),
		GinOnlyErrorHandler(c.Task.NextWork, http2.StatusNoContent))
	v1.PUT("/service-order/:id/work/:workId/cancel", middleware.Auth(auth.MechanicRoles...),
		GinOnlyErrorHandler(c.Task.CancelWork, http2.StatusNoContent))
	v1.PUT("/service-order/:id/finish", middleware.Auth(auth.MechanicRoles...),
		GinOnlyErrorHandler(c.Task.Finish, http2.StatusNoContent))

	return router
}

// logFailure keeps the full error (with its root cause) in the server log for 5xx and for
// infrastructure 409/422, whose messages are hidden from the client.
func logFailure(ctx context.Context, status int, err error) {
	if status >= http2.StatusInternalServerError || web.IsInfraError(err) {
		logger.Of(ctx).Error(err, zap.Int("status", status))
	}
}

func mountSwaggerUI(router *gin.Engine) {
	router.GET("/swagger.yaml", func(c *gin.Context) {
		c.Data(http2.StatusOK, "application/yaml; charset=utf-8", api.SwaggerBytes)
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger.yaml")))
}

// GinOnlyErrorHandler adapts a func(ctx, req) error controller to gin.
func GinOnlyErrorHandler(cb func(context.Context, *http2.Request) error, customStatus ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		for _, v := range c.Params {
			ctx = context.WithValue(ctx, v.Key, v.Value) //nolint:staticcheck
		}

		if err := cb(ctx, c.Request); err != nil {
			status, errResp := web.Error(err)
			logFailure(ctx, status, err)
			c.JSON(status, errResp)
			return
		}

		if len(customStatus) == 0 {
			c.Status(http2.StatusNoContent)
			c.Writer.WriteHeaderNow()
			return
		}

		c.Status(customStatus[0])
		c.Writer.WriteHeaderNow()
	}
}

// GinHandler adapts a func(ctx, req) (T, error) controller to gin.
func GinHandler[T any](cb func(context.Context, *http2.Request) (T, error), customStatus ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		for _, v := range c.Params {
			ctx = context.WithValue(ctx, v.Key, v.Value) //nolint:staticcheck
		}

		resp, err := cb(ctx, c.Request)
		if err != nil {
			status, errResp := web.Error(err)
			logFailure(ctx, status, err)
			c.JSON(status, errResp)
			return
		}

		if len(customStatus) == 0 {
			c.JSON(http2.StatusOK, resp)
			return
		}

		c.JSON(customStatus[0], resp)
	}
}
