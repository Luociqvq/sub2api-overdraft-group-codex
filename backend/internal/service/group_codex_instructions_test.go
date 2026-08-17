package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyGroupCodexInstructions(t *testing.T) {
	group := &Group{CodexInstructionsEnabled: true, CodexInstructions: "group rules"}
	tests := []struct {
		name     string
		endpoint string
		body     string
		assert   func(t *testing.T, body map[string]any)
	}{
		{
			name: "responses merges top level instructions", endpoint: "responses",
			body: `{"model":"gpt-5","instructions":"client rules"}`,
			assert: func(t *testing.T, body map[string]any) {
				require.Equal(t, "group rules\n\nclient rules", body["instructions"])
			},
		},
		{
			name: "chat prepends system message", endpoint: "chat_completions",
			body: `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`,
			assert: func(t *testing.T, body map[string]any) {
				messages := body["messages"].([]any)
				require.Equal(t, "system", messages[0].(map[string]any)["role"])
				require.Equal(t, "group rules", messages[0].(map[string]any)["content"])
			},
		},
		{
			name: "anthropic prepends system block", endpoint: "messages",
			body: `{"model":"claude","system":[{"type":"text","text":"client"}]}`,
			assert: func(t *testing.T, body map[string]any) {
				blocks := body["system"].([]any)
				require.Equal(t, "group rules", blocks[0].(map[string]any)["text"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := ApplyGroupCodexInstructions([]byte(tt.body), tt.endpoint, group)
			require.NoError(t, err)
			require.True(t, changed)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(out, &decoded))
			tt.assert(t, decoded)
		})
	}
}

func TestApplyGroupCodexInstructionsDisabledIsNoop(t *testing.T) {
	body := []byte(`{"model":"gpt-5"}`)
	out, changed, err := ApplyGroupCodexInstructions(body, "responses", &Group{CodexInstructions: "rules"})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, out)
}

func TestApplyGroupCodexInstructionsIsIdempotent(t *testing.T) {
	group := &Group{CodexInstructionsEnabled: true, CodexInstructions: "group rules"}
	tests := []struct {
		endpoint string
		body     string
	}{
		{endpoint: "responses", body: `{"model":"gpt-5","instructions":"client rules"}`},
		{endpoint: "chat_completions", body: `{"messages":[{"role":"system","content":"client rules"}]}`},
		{endpoint: "messages", body: `{"system":[{"type":"text","text":"client rules"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			first, changed, err := ApplyGroupCodexInstructions([]byte(tt.body), tt.endpoint, group)
			require.NoError(t, err)
			require.True(t, changed)

			second, changed, err := ApplyGroupCodexInstructions(first, tt.endpoint, group)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, first, second)
		})
	}
}

func TestApplyGroupCodexInstructionsRejectsTrailingJSON(t *testing.T) {
	body := []byte(`{"model":"gpt-5"} {"extra":true}`)
	out, changed, err := ApplyGroupCodexInstructions(body, "responses", &Group{
		CodexInstructionsEnabled: true,
		CodexInstructions:        "group rules",
	})
	require.Error(t, err)
	require.False(t, changed)
	require.Equal(t, body, out)
}
