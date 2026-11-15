//go:build integration

// Integration tests against real Postgres (and MiniStack for the messaging test).
// Env: POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB, POSTGRES_SSLMODE.
// The SNS/SQS test runs only when AWS_ENDPOINT_URL is set (CI sets it for MiniStack).
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task/repository"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	soID          = "01JD8Z4R3Q8M2V7K5N9XWABCDE"
	workA         = "11111111-1111-4111-8111-000000000001"
	workB         = "11111111-1111-4111-8111-000000000002"
	twoWorksJSON  = `{"works":[{"work_id":"` + workA + `","name":"Troca de óleo"},{"work_id":"` + workB + `","name":"Filtro"}]}`
	oneWorkJSON   = `{"works":[{"work_id":"` + workA + `","name":"Troca de óleo"}]}`
	cancelPayload = `{}`
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD"), os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_PORT"), os.Getenv("POSTGRES_DB"), os.Getenv("POSTGRES_SSLMODE"))
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, database.Ping())
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec("TRUNCATE execution_task, execution_work, execution_work_history, processed_message, outbox CASCADE")
	require.NoError(t, err)
	return database
}

// fakeQueue satisfies messaging.Queue for tests that feed messages to the consumer directly.
type fakeQueue struct{}

func (fakeQueue) ReceiveMessage(context.Context, string, int32, int32) ([]messaging.Message, error) {
	return nil, nil
}

func (fakeQueue) DeleteMessage(context.Context, string, string) error { return nil }

type harness struct {
	t        *testing.T
	db       *sql.DB
	consumer *messaging.Consumer
	router   http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	database := openDB(t)
	idGen := id.NewIDGenerator()
	svc := task.NewService(database, repository.NewPostgres(idGen), idGen.NewUUID)
	router := routing.SetupRouter(&container.HandlersWrapper{Task: task.NewController(svc)}, &container.Middlewares{})
	consumer := messaging.NewConsumer(database, fakeQueue{}, "unused")
	svc.RegisterHandlers(consumer)
	return &harness{t: t, db: database, consumer: consumer, router: router}
}

// deliver hands a command to the consumer exactly as an SQS message body would arrive.
func (h *harness) deliver(typ, messageID, saga, payload string) {
	h.t.Helper()
	env := messaging.Envelope{
		MessageID:      messageID,
		SagaID:         saga,
		Type:           typ,
		OccurredAt:     time.Now().UTC(),
		ServiceOrderID: soID,
		RequestID:      "req-it",
		Payload:        json.RawMessage(payload),
	}
	body, err := json.Marshal(env)
	require.NoError(h.t, err)
	require.NoError(h.t, h.consumer.Handle(context.Background(), messaging.Message{Body: string(body), ReceiptHandle: "r-" + messageID}))
}

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	require.NoError(h.t, h.db.QueryRow(query, args...).Scan(&n))
	return n
}

type outboxRow struct {
	Type   string
	SagaID string
}

// outboxFor returns the events published for the service order, in order.
func (h *harness) outboxFor() []outboxRow {
	h.t.Helper()
	rows, err := h.db.Query(`SELECT type, body->>'saga_id' FROM outbox WHERE body->>'service_order_id' = $1 ORDER BY id`, soID)
	require.NoError(h.t, err)
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		require.NoError(h.t, rows.Scan(&r.Type, &r.SagaID))
		out = append(out, r)
	}
	require.NoError(h.t, rows.Err())
	return out
}

func (h *harness) mechanicToken() string {
	h.t.Helper()
	tok, err := auth.GenerateToken("mechanic-1", []auth.Role{auth.MECHANIC}, time.Now().Add(time.Hour))
	require.NoError(h.t, err)
	return "Bearer " + tok
}

func (h *harness) rest(method, path string) int {
	h.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", h.mechanicToken())
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec.Code
}

func TestEnqueueExecutionIsIdempotentPerMessage(t *testing.T) {
	h := newHarness(t)
	const msgID = "7c1a0000-0000-4000-8000-000000000101"

	h.deliver("EnqueueExecution", msgID, "saga-it-idem", twoWorksJSON)
	h.deliver("EnqueueExecution", msgID, "saga-it-idem", twoWorksJSON) // same message_id again

	assert.Equal(t, 1, h.count(`SELECT count(*) FROM execution_task WHERE service_order_id = $1`, soID))
	assert.Equal(t, 2, h.count(`SELECT count(*) FROM execution_work`))
	assert.Equal(t, 1, h.count(`SELECT count(*) FROM processed_message WHERE message_id = $1`, msgID))

	queued := 0
	for _, r := range h.outboxFor() {
		if r.Type == "ExecutionQueued" {
			queued++
		}
	}
	assert.Equal(t, 1, queued)
}

func TestEnqueueExecutionWithNewMessageForActiveTaskIsNoop(t *testing.T) {
	h := newHarness(t)
	h.deliver("EnqueueExecution", "7c1a0000-0000-4000-8000-000000000111", "saga-it-dup", oneWorkJSON)
	before := len(h.outboxFor())

	h.deliver("EnqueueExecution", "7c1a0000-0000-4000-8000-000000000112", "saga-it-dup", oneWorkJSON)
	assert.Len(t, h.outboxFor(), before, "a second command for an active task publishes nothing")
	assert.Equal(t, 1, h.count(`SELECT count(*) FROM execution_task WHERE service_order_id = $1`, soID))
}

func TestInvalidEnqueuePublishesExecutionFailed(t *testing.T) {
	h := newHarness(t)
	h.deliver("EnqueueExecution", "7c1a0000-0000-4000-8000-000000000121", "saga-it-invalid", `{"works":[]}`)

	assert.Equal(t, 0, h.count(`SELECT count(*) FROM execution_task WHERE service_order_id = $1`, soID))
	events := h.outboxFor()
	require.Len(t, events, 1)
	assert.Equal(t, "ExecutionFailed", events[0].Type)
	assert.Equal(t, "saga-it-invalid", events[0].SagaID)
}

// The saga of EnqueueExecution is kept on every event of the task, including those triggered by REST.
func TestRESTFlowKeepsCommandSagaID(t *testing.T) {
	h := newHarness(t)
	const saga = "saga-it-rest"
	h.deliver("EnqueueExecution", "7c1a0000-0000-4000-8000-000000000131", saga, twoWorksJSON)

	base := "/v1/service-order/" + soID
	assert.Equal(t, http.StatusNoContent, h.rest(http.MethodPut, base+"/work/"+workA+"/next"))
	assert.Equal(t, http.StatusNoContent, h.rest(http.MethodPut, base+"/work/"+workA+"/next"))
	assert.Equal(t, http.StatusConflict, h.rest(http.MethodPut, base+"/finish"), "work B still awaiting start")
	assert.Equal(t, http.StatusNoContent, h.rest(http.MethodPut, base+"/work/"+workB+"/cancel"))
	assert.Equal(t, http.StatusNoContent, h.rest(http.MethodPut, base+"/finish"))

	events := h.outboxFor()
	require.NotEmpty(t, events)
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
		assert.Equal(t, saga, e.SagaID, "event %s must carry the EnqueueExecution saga", e.Type)
	}
	assert.Contains(t, types, "ExecutionCompleted")
	assert.Equal(t, "COMPLETED", queryTaskStatus(t, h, soID))
}

func TestCancelExecutionCancelsActiveWorks(t *testing.T) {
	h := newHarness(t)
	h.deliver("EnqueueExecution", "7c1a0000-0000-4000-8000-000000000141", "saga-it-cancel", twoWorksJSON)
	h.deliver("CancelExecution", "7c1a0000-0000-4000-8000-000000000142", "saga-it-cancel-2", cancelPayload)

	assert.Equal(t, "CANCELLED", queryTaskStatus(t, h, soID))
	assert.Equal(t, 2, h.count(`SELECT count(*) FROM execution_work WHERE status = 'CANCELLED'`))
	assert.Equal(t, 2, h.count(`SELECT count(*) FROM execution_work_history WHERE new_status = 'CANCELLED'`))
	// Enqueue published 2 creations and ExecutionQueued; cancel published 2 work changes after them.
	events := h.outboxFor()
	require.Len(t, events, 5)
	for _, e := range events[3:] {
		assert.Equal(t, "ExecutionWorkStatusChanged", e.Type)
		assert.Equal(t, "saga-it-cancel", e.SagaID, "task events keep the saga of EnqueueExecution")
	}
}

func queryTaskStatus(t *testing.T, h *harness, serviceOrderID string) string {
	t.Helper()
	var status string
	require.NoError(t, h.db.QueryRow(`SELECT status FROM execution_task WHERE service_order_id = $1`, serviceOrderID).Scan(&status))
	return status
}

// --- messaging (SNS/SQS on MiniStack; skipped when AWS_ENDPOINT_URL is not set) ---

func TestEnqueueExecutionPublishesThroughSNSAndSQS(t *testing.T) {
	if os.Getenv("AWS_ENDPOINT_URL") == "" {
		t.Skip("AWS_ENDPOINT_URL not set: messaging integration runs in CI against MiniStack")
	}
	ctx := context.Background()
	database := openDB(t)

	snsClient, err := messaging.NewSNSClient(ctx)
	require.NoError(t, err)
	sqsClient, err := messaging.NewSQSClient(ctx)
	require.NoError(t, err)

	suffix := fmt.Sprint(time.Now().UnixNano())
	commandsARN := createTopic(t, ctx, snsClient, "it-saga-commands-"+suffix)
	eventsARN := createTopic(t, ctx, snsClient, "it-execution-events-"+suffix)
	executionQ := createQueue(t, ctx, sqsClient, "it-execution-"+suffix)
	subscribe(t, ctx, snsClient, sqsClient, commandsARN, executionQ, `{"target":["execution"]}`)
	testQ := createQueue(t, ctx, sqsClient, "it-test-"+suffix)
	subscribe(t, ctx, snsClient, sqsClient, eventsARN, testQ, "")

	idGen := id.NewIDGenerator()
	svc := task.NewService(database, repository.NewPostgres(idGen), idGen.NewUUID)
	consumer := messaging.NewConsumer(database, messaging.NewSQSQueue(sqsClient), executionQ)
	svc.RegisterHandlers(consumer)
	relay := messaging.NewRelay(database, messaging.NewSNSPublisher(snsClient), map[string]string{"execution-events": eventsARN})
	runCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() { _ = consumer.Run(runCtx) }()
	go func() { _ = relay.Run(runCtx) }()

	env := messaging.Envelope{
		MessageID:      "7c1a0000-0000-4000-8000-000000000151",
		SagaID:         "saga-it-sqs",
		Type:           "EnqueueExecution",
		OccurredAt:     time.Now().UTC(),
		ServiceOrderID: soID,
		RequestID:      "req-it",
		Payload:        json.RawMessage(oneWorkJSON),
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	_, err = snsClient.Publish(ctx, &awssns.PublishInput{
		TopicArn: aws.String(commandsARN),
		Message:  aws.String(string(body)),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			"target": {DataType: aws.String("String"), StringValue: aws.String("execution")},
			"type":   {DataType: aws.String("String"), StringValue: aws.String("EnqueueExecution")},
		},
	})
	require.NoError(t, err)

	queued := receiveEvent(t, ctx, sqsClient, testQ, "ExecutionQueued")
	assert.Equal(t, "saga-it-sqs", queued.SagaID)
	assert.Equal(t, soID, queued.ServiceOrderID)
}

func createTopic(t *testing.T, ctx context.Context, c *awssns.Client, name string) string {
	t.Helper()
	out, err := c.CreateTopic(ctx, &awssns.CreateTopicInput{Name: aws.String(name)})
	require.NoError(t, err)
	return aws.ToString(out.TopicArn)
}

func createQueue(t *testing.T, ctx context.Context, c *awssqs.Client, name string) string {
	t.Helper()
	out, err := c.CreateQueue(ctx, &awssqs.CreateQueueInput{QueueName: aws.String(name)})
	require.NoError(t, err)
	return aws.ToString(out.QueueUrl)
}

// subscribe links topic -> queue with a queue policy, raw delivery and an optional filter policy.
func subscribe(t *testing.T, ctx context.Context, sns *awssns.Client, sqs *awssqs.Client, topicARN, queueURL, filter string) {
	t.Helper()
	attrs, err := sqs.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	queueARN := attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"sqs:SendMessage","Resource":"%s","Condition":{"ArnEquals":{"aws:SourceArn":"%s"}}}]}`, queueARN, topicARN)
	_, err = sqs.SetQueueAttributes(ctx, &awssqs.SetQueueAttributesInput{QueueUrl: aws.String(queueURL), Attributes: map[string]string{"Policy": policy}})
	require.NoError(t, err)

	sub, err := sns.Subscribe(ctx, &awssns.SubscribeInput{TopicArn: aws.String(topicARN), Protocol: aws.String("sqs"), Endpoint: aws.String(queueARN)})
	require.NoError(t, err)
	_, err = sns.SetSubscriptionAttributes(ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("RawMessageDelivery"), AttributeValue: aws.String("true")})
	require.NoError(t, err)
	if filter != "" {
		_, err = sns.SetSubscriptionAttributes(ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("FilterPolicyScope"), AttributeValue: aws.String("MessageAttributes")})
		require.NoError(t, err)
		_, err = sns.SetSubscriptionAttributes(ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("FilterPolicy"), AttributeValue: aws.String(filter)})
		require.NoError(t, err)
	}
}

// receiveEvent polls the test queue until an event of the given type arrives.
func receiveEvent(t *testing.T, ctx context.Context, sqs *awssqs.Client, queueURL, typ string) messaging.Envelope {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := sqs.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 10, WaitTimeSeconds: 2})
		require.NoError(t, err)
		for _, m := range out.Messages {
			env, err := messaging.Decode([]byte(aws.ToString(m.Body)))
			require.NoError(t, err)
			if env.Type == typ {
				return env
			}
		}
	}
	t.Fatalf("event %s not received", typ)
	return messaging.Envelope{}
}
