package bootstrap

import (
	"context"
	"database/sql"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/env"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task/repository"
	"go.uber.org/zap"
)

const topicExecutionEvents = "execution-events"

// Servers returns the HTTP API and, when configured, the outbox relay and SQS consumer.
func Servers() []app.Server {
	database := app.DBConnect()

	idGen := id.NewIDGenerator()
	service := task.NewService(database, repository.NewPostgres(idGen), idGen.NewUUID)

	router := routing.SetupRouter(
		&container.HandlersWrapper{Task: task.NewController(service)},
		middlewaresContainer(),
	)

	servers := []app.Server{app.NewHTTPServer("http-api", ":"+env.GetString("PORT", "8084"), router)}
	return append(servers, messagingWorkers(database, service)...)
}

// messagingWorkers starts the relay and the consumer. They are disabled when the queue or topic is not configured.
func messagingWorkers(database *sql.DB, service *task.Service) []app.Server {
	queueURL := env.GetString("EXECUTION_QUEUE_URL", "")
	eventsARN := env.GetString("EXECUTION_EVENTS_TOPIC_ARN", "")
	if queueURL == "" || eventsARN == "" {
		logger.Global().Warn("messaging disabled: EXECUTION_QUEUE_URL or EXECUTION_EVENTS_TOPIC_ARN is empty")
		return nil
	}

	ctx := context.Background()
	snsClient, err := messaging.NewSNSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SNS client", zap.Error(err))
	}
	sqsClient, err := messaging.NewSQSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SQS client", zap.Error(err))
	}

	relay := messaging.NewRelay(database, messaging.NewSNSPublisher(snsClient), map[string]string{topicExecutionEvents: eventsARN})
	consumer := messaging.NewConsumer(database, messaging.NewSQSQueue(sqsClient), queueURL)
	service.RegisterHandlers(consumer)

	return []app.Server{
		newWorker("outbox-relay", relay.Run),
		newWorker("sqs-consumer", consumer.Run),
	}
}

func middlewaresContainer() *container.Middlewares {
	return &container.Middlewares{
		"Logger":       middleware.Logger(),
		"Recovery":     middleware.Recovery(),
		"Metrics":      middleware.Metrics(),
		"ErrorHandler": middleware.ErrorHandler(),
	}
}
