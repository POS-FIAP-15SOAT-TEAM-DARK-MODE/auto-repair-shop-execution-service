package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture loads testdata/contracts/<name>.json and returns the raw envelope and its payload.
func fixture(t *testing.T, name string) ([]byte, json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", name+".json"))
	require.NoError(t, err)
	var env struct {
		Payload json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	return raw, env.Payload
}

// Consumed commands: the fixtures must decode into the handler payloads.
func TestContract_ConsumedCommands(t *testing.T) {
	_, payload := fixture(t, "EnqueueExecution")
	var p enqueuePayload
	require.NoError(t, json.Unmarshal(payload, &p))
	require.Len(t, p.Works, 1)
	assert.Equal(t, "11111111-1111-4111-8111-000000000001", p.Works[0].WorkID)
	assert.Equal(t, "Troca de óleo", p.Works[0].Name)

	_, payload = fixture(t, "CancelExecution")
	assert.JSONEq(t, "{}", string(payload))
}

// Published events: the payloads built by execution must equal the fixtures.
func TestContract_PublishedEvents(t *testing.T) {
	const taskID = "d4f7a1b2-3c5e-4f60-8a9b-1c2d3e4f5a6b"
	created := "AWAITING_START"
	prevAwaiting := "AWAITING_START"

	cases := map[string]struct {
		fixture string
		payload any
	}{
		"ExecutionQueued":                    {"ExecutionQueued", taskPayload{TaskID: taskID}},
		"ExecutionCompleted":                 {"ExecutionCompleted", taskPayload{TaskID: taskID}},
		"ExecutionFailed":                    {"ExecutionFailed", executionFailedPayload{TaskID: taskID, Reason: reasonAllWorksCancelled}},
		"ExecutionFailed_invalid":            {"ExecutionFailed_invalid", executionFailedPayload{TaskID: "", Reason: reasonInvalidRequest}},
		"ExecutionWorkStatusChanged":         {"ExecutionWorkStatusChanged", workStatusChangedPayload{WorkID: "11111111-1111-4111-8111-000000000001", WorkName: "Troca de óleo", Previous: &prevAwaiting, New: "IN_PROGRESS"}},
		"ExecutionWorkStatusChanged_created": {"ExecutionWorkStatusChanged_created", workStatusChangedPayload{WorkID: "11111111-1111-4111-8111-000000000001", WorkName: "Troca de óleo", Previous: nil, New: created}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, want := fixture(t, tc.fixture)
			got, err := json.Marshal(tc.payload)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
}

func TestContract_EnvelopeOfEventsCarriesSagaFields(t *testing.T) {
	raw, _ := fixture(t, "ExecutionQueued")
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	for _, k := range []string{"message_id", "saga_id", "type", "occurred_at", "service_order_id", "request_id", "payload"} {
		assert.Contains(t, m, k)
	}
}
