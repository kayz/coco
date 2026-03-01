package agent

import (
	"encoding/json"
	"testing"

	"github.com/sashabaranov/go-openai"
)

func TestOpenAIToolCodecRoundTrip(t *testing.T) {
	tools := []Tool{
		{Name: "ai.list_models"},
		{Name: "ai-switch-model"},
		{Name: "ai list models"},
	}

	codec := newOpenAIToolCodec(tools)
	if codec == nil {
		t.Fatal("codec should not be nil")
	}

	for _, tool := range tools {
		apiName := codec.encode(tool.Name)
		if !openAIToolNamePattern.MatchString(apiName) {
			t.Fatalf("encoded tool name is invalid: %q -> %q", tool.Name, apiName)
		}
		if decoded := codec.decode(apiName); decoded != tool.Name {
			t.Fatalf("decode mismatch: got %q want %q", decoded, tool.Name)
		}
	}
}

func TestOpenAIMessageFromGenericKeepsReasoningAndMapsToolName(t *testing.T) {
	codec := newOpenAIToolCodec([]Tool{{Name: "ai.list_models"}})
	msg := Message{
		Role:             "assistant",
		Content:          "planning",
		ReasoningContent: "hidden-thought",
		ToolCalls: []ToolCall{
			{
				ID:    "call_1",
				Name:  "ai.list_models",
				Input: json.RawMessage(`{"x":1}`),
			},
		},
	}

	converted := openAIMessageFromGeneric(msg, codec)
	if converted.ReasoningContent != "hidden-thought" {
		t.Fatalf("reasoning content not preserved: %q", converted.ReasoningContent)
	}
	if len(converted.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(converted.ToolCalls))
	}
	if converted.ToolCalls[0].Function.Name != codec.encode("ai.list_models") {
		t.Fatalf("tool name not encoded: %q", converted.ToolCalls[0].Function.Name)
	}
}

func TestGenericResponseFromOpenAIDecodesToolNameAndReasoning(t *testing.T) {
	codec := newOpenAIToolCodec([]Tool{{Name: "ai.get_current_model"}})
	apiName := codec.encode("ai.get_current_model")

	resp := openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: openai.FinishReasonToolCalls,
				Message: openai.ChatCompletionMessage{
					Content:          "ok",
					ReasoningContent: "reasoning",
					ToolCalls: []openai.ToolCall{
						{
							ID:   "1",
							Type: openai.ToolTypeFunction,
							Function: openai.FunctionCall{
								Name:      apiName,
								Arguments: `{"foo":"bar"}`,
							},
						},
					},
				},
			},
		},
	}

	got := genericResponseFromOpenAI(resp, codec)
	if got.ReasoningContent != "reasoning" {
		t.Fatalf("reasoning content mismatch: %q", got.ReasoningContent)
	}
	if got.FinishReason != "tool_use" {
		t.Fatalf("finish reason mismatch: %q", got.FinishReason)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "ai.get_current_model" {
		t.Fatalf("tool name decode mismatch: %+v", got.ToolCalls)
	}
}
