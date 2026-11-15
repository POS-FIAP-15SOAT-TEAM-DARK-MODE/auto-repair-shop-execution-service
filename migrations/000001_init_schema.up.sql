-- execution: task and work progress (contracts/ddl.md, section execution)

CREATE TABLE execution_task (
    id               VARCHAR(36) PRIMARY KEY,
    service_order_id VARCHAR(26) NOT NULL UNIQUE,
    saga_id          VARCHAR(36) NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'QUEUED'
                     CHECK (status IN ('QUEUED', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED')),
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    started_at       TIMESTAMP WITH TIME ZONE,
    completed_at     TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_execution_task_status_created ON execution_task (status, created_at);

CREATE TABLE execution_work (
    id        VARCHAR(36) PRIMARY KEY,
    task_id   VARCHAR(36) NOT NULL REFERENCES execution_task (id) ON DELETE CASCADE,
    work_id   VARCHAR(36) NOT NULL,
    name      VARCHAR(255) NOT NULL,
    status    VARCHAR(20) NOT NULL DEFAULT 'AWAITING_START'
              CHECK (status IN ('AWAITING_START', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED')),
    UNIQUE (task_id, work_id)
);

CREATE TABLE execution_work_history (
    id              VARCHAR(26) PRIMARY KEY,
    task_id         VARCHAR(36) NOT NULL REFERENCES execution_task (id) ON DELETE CASCADE,
    work_id         VARCHAR(36) NOT NULL,
    previous_status VARCHAR(20),
    new_status      VARCHAR(20) NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_execution_work_history_task_work ON execution_work_history (task_id, work_id);

-- dedupe de consumidor
CREATE TABLE processed_message (
    message_id   UUID PRIMARY KEY,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- outbox (body = envelope JSON completo, contracts/messaging.md)
CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    message_id   UUID NOT NULL UNIQUE,
    topic        VARCHAR(50) NOT NULL,
    target       VARCHAR(20),
    type         VARCHAR(50) NOT NULL,
    body         JSONB NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
