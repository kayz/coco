package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sashabaranov/go-openai"
)

var openAIToolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type openAIToolCodec struct {
	toAPI   map[string]string
	fromAPI map[string]string
}

func newOpenAIToolCodec(tools []Tool) *openAIToolCodec {
	c := &openAIToolCodec{
		toAPI:   make(map[string]string, len(tools)),
		fromAPI: make(map[string]string, len(tools)),
	}
	used := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		apiName := sanitizeOpenAIToolName(tool.Name)
		base := apiName
		if base == "" {
			base = "tool"
			apiName = base
		}
		for i := 2; ; i++ {
			if _, exists := used[apiName]; !exists {
				break
			}
			apiName = fmt.Sprintf("%s_%d", base, i)
		}
		c.toAPI[tool.Name] = apiName
		c.fromAPI[apiName] = tool.Name
		used[apiName] = struct{}{}
	}
	return c
}

func sanitizeOpenAIToolName(name string) string {
	name = strings.TrimSpace(name)
	if openAIToolNamePattern.MatchString(name) {
		return name
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_-")
	if s == "" {
		return "tool"
	}
	return s
}

func (c *openAIToolCodec) encode(name string) string {
	if c == nil {
		return name
	}
	if v, ok := c.toAPI[name]; ok {
		return v
	}
	return sanitizeOpenAIToolName(name)
}

func (c *openAIToolCodec) decode(name string) string {
	if c == nil {
		return name
	}
	if v, ok := c.fromAPI[name]; ok {
		return v
	}
	return name
}

func openAIToolsFromGeneric(tools []Tool, codec *openAIToolCodec) []openai.Tool {
	converted := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		var params map[string]any
		if err := json.Unmarshal(tool.InputSchema, &params); err != nil {
			params = map[string]any{"type": "object"}
		}
		converted = append(converted, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        codec.encode(tool.Name),
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	return converted
}

func openAIMessageFromGeneric(msg Message, codec *openAIToolCodec) openai.ChatCompletionMessage {
	switch msg.Role {
	case "user":
		if msg.ToolResult != nil {
			content := msg.ToolResult.Content
			if content == "" {
				content = "(empty)"
			}
			return openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    content,
				ToolCallID: msg.ToolResult.ToolCallID,
			}
		}
		return openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: msg.Content,
		}

	case "assistant":
		m := openai.ChatCompletionMessage{
			Role:             openai.ChatMessageRoleAssistant,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
		}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = make([]openai.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				m.ToolCalls[i] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      codec.encode(tc.Name),
						Arguments: string(tc.Input),
					},
				}
			}
		}
		return m

	case "tool":
		content := msg.Content
		if content == "" && msg.ToolResult != nil {
			content = msg.ToolResult.Content
		}
		if content == "" {
			content = "(empty)"
		}
		toolCallID := ""
		if msg.ToolResult != nil {
			toolCallID = msg.ToolResult.ToolCallID
		}
		return openai.ChatCompletionMessage{
			Role:       openai.ChatMessageRoleTool,
			Content:    content,
			ToolCallID: toolCallID,
		}

	default:
		return openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: msg.Content,
		}
	}
}

func genericResponseFromOpenAI(resp openai.ChatCompletionResponse, codec *openAIToolCodec) ChatResponse {
	if len(resp.Choices) == 0 {
		return ChatResponse{}
	}

	choice := resp.Choices[0]
	var toolCalls []ToolCall

	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:    tc.ID,
			Name:  codec.decode(tc.Function.Name),
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	finishReason := "stop"
	if choice.FinishReason == openai.FinishReasonToolCalls {
		finishReason = "tool_use"
	}

	return ChatResponse{
		Content:          choice.Message.Content,
		ToolCalls:        toolCalls,
		ReasoningContent: choice.Message.ReasoningContent,
		FinishReason:     finishReason,
	}
}
