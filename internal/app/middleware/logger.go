package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/logger"
)

// RequestIDHeader is the header that carries the request_id across services.
const RequestIDHeader = "X-Request-Id"

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.Contains(path, "swagger") {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		ctx, reqId := logger.Request(ctx, c.GetHeader(RequestIDHeader))
		c.Request = c.Request.WithContext(ctx)
		// Handlers read the id from the request header, so the message envelopes they
		// publish carry the same request_id as the log lines of the request.
		c.Request.Header.Set(RequestIDHeader, reqId)

		c.Writer.Header().Set(RequestIDHeader, reqId)

		start := time.Now()
		query := c.Request.URL.RawQuery
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
		}

		logger.Of(ctx).Info("request.started", fields...)

		c.Next()

		fields = append(fields,
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)))

		if len(c.Errors) > 0 {
			for _, e := range c.Errors.Errors() {
				logger.Of(ctx).Error(errors.New(e), fields...)
			}
			return
		}

		logger.Of(ctx).Info("request.completed", fields...)
	}
}
