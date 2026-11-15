package task

// Payloads of the commands consumed and the events published by execution (contracts/messaging.md).

const (
	topicExecutionEvents = "execution-events"

	cmdEnqueueExecution = "EnqueueExecution"
	cmdCancelExecution  = "CancelExecution"

	evExecutionQueued            = "ExecutionQueued"
	evExecutionWorkStatusChanged = "ExecutionWorkStatusChanged"
	evExecutionCompleted         = "ExecutionCompleted"
	evExecutionFailed            = "ExecutionFailed"
	reasonInvalidRequest         = "INVALID_REQUEST"
	reasonAllWorksCancelled      = "ALL_WORKS_CANCELLED"
)

type enqueuePayload struct {
	Works []enqueueWorkPayload `json:"works"`
}

type enqueueWorkPayload struct {
	WorkID string `json:"work_id"`
	Name   string `json:"name"`
}

type taskPayload struct {
	TaskID string `json:"task_id"`
}

type executionFailedPayload struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

type workStatusChangedPayload struct {
	WorkID   string  `json:"work_id"`
	WorkName string  `json:"work_name"`
	Previous *string `json:"previous"`
	New      string  `json:"new"`
}
