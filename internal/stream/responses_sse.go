package stream

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponsesStreamHandler 处理 Amazon Q 事件并转换为 OpenAI Responses API SSE 格式
type ResponsesStreamHandler struct {
	ResponseID  string
	Model       string
	CreatedAt   int64
	MessageID   string
	OutputIndex int

	Started     bool
	TextStarted bool

	ResponseBuffer []string

	// 工具调用状态
	ToolCalls           []responsesToolCallInfo
	CurrentToolCall     *responsesToolCallBuildState
	ProcessedToolUseIDs map[string]bool

	// token 计数
	OutputDeltaCount  int
	ContentCharCount  int
	PendingKiroBuffer string

	// thinking 过滤
	InThinking  bool
	ThinkBuffer string
}

type responsesToolCallInfo struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

type responsesToolCallBuildState struct {
	ID            string
	CallID        string
	Name          string
	OutputIndex   int
	ArgumentsJSON []string
}

// NewResponsesStreamHandler 创建 Responses API 流处理器
func NewResponsesStreamHandler(responseID, model string) *ResponsesStreamHandler {
	return &ResponsesStreamHandler{
		ResponseID:          responseID,
		Model:               model,
		CreatedAt:           time.Now().Unix(),
		MessageID:           "msg_" + responseID[5:],
		ProcessedToolUseIDs: make(map[string]bool),
	}
}

func (h *ResponsesStreamHandler) buildEvent(eventType string, data map[string]interface{}) string {
	data["type"] = eventType
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))
}

func (h *ResponsesStreamHandler) buildResponseObject(status string, output []interface{}, usage map[string]interface{}) map[string]interface{} {
	resp := map[string]interface{}{
		"id":         h.ResponseID,
		"object":     "response",
		"created_at": h.CreatedAt,
		"model":      h.Model,
		"status":     status,
		"output":     output,
	}
	if usage != nil {
		resp["usage"] = usage
	}
	return resp
}

func (h *ResponsesStreamHandler) ensureStarted() []string {
	if h.Started {
		return nil
	}
	h.Started = true
	return []string{h.buildEvent("response.created", map[string]interface{}{
		"response": h.buildResponseObject("in_progress", []interface{}{}, nil),
	})}
}

// HandleEvent 处理单个 Amazon Q 事件并返回 Responses API SSE 事件
func (h *ResponsesStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	switch eventType {
	case "initial-response":
		events = append(events, h.ensureStarted()...)

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)
		if content == "" {
			break
		}

		// 过滤 <thinking>...</thinking> 内容
		content = h.filterThinking(content)
		if content == "" {
			break
		}

		events = append(events, h.ensureStarted()...)

		// 首次文本：发送 output_item.added 和 content_part.added
		if !h.TextStarted {
			h.TextStarted = true
			h.OutputIndex = 0

			events = append(events, h.buildEvent("response.output_item.added", map[string]interface{}{
				"output_index": 0,
				"item": map[string]interface{}{
					"type":    "message",
					"id":      h.MessageID,
					"status":  "in_progress",
					"role":    "assistant",
					"content": []interface{}{},
				},
			}))

			events = append(events, h.buildEvent("response.content_part.added", map[string]interface{}{
				"item_id":       h.MessageID,
				"output_index":  0,
				"content_index": 0,
				"part": map[string]interface{}{
					"type":        "output_text",
					"text":        "",
					"annotations": []interface{}{},
				},
			}))
		}

		// 品牌名替换
		content, h.ContentCharCount, h.PendingKiroBuffer = replaceOpenAIBrandInContent(content, h.ContentCharCount, h.PendingKiroBuffer)

		h.OutputDeltaCount++
		h.ResponseBuffer = append(h.ResponseBuffer, content)

		events = append(events, h.buildEvent("response.output_text.delta", map[string]interface{}{
			"item_id":       h.MessageID,
			"output_index":  0,
			"content_index": 0,
			"delta":         content,
		}))

	case "toolUseEvent":
		toolCallID, _ := payload["toolUseId"].(string)
		toolName, _ := payload["name"].(string)
		toolInput := payload["input"]
		isStop, _ := payload["stop"].(bool)

		events = append(events, h.ensureStarted()...)

		// 开始新的工具调用
		if toolCallID != "" && toolName != "" && h.CurrentToolCall == nil {
			if !h.ProcessedToolUseIDs[toolCallID] {
				h.ProcessedToolUseIDs[toolCallID] = true

				// 先结束文本内容
				if h.TextStarted {
					events = append(events, h.finalizeTextContent()...)
				}

				h.OutputIndex++
				fcID := "fc_" + toolCallID

				h.CurrentToolCall = &responsesToolCallBuildState{
					ID:          fcID,
					CallID:      toolCallID,
					Name:        toolName,
					OutputIndex: h.OutputIndex,
				}

				events = append(events, h.buildEvent("response.output_item.added", map[string]interface{}{
					"output_index": h.OutputIndex,
					"item": map[string]interface{}{
						"type":      "function_call",
						"id":        fcID,
						"call_id":   toolCallID,
						"name":      toolName,
						"arguments": "",
						"status":    "in_progress",
					},
				}))
			}
		}

		// 累积工具输入
		if h.CurrentToolCall != nil && toolInput != nil {
			var fragment string
			switch v := toolInput.(type) {
			case string:
				fragment = v
			default:
				b, _ := json.Marshal(v)
				fragment = string(b)
			}

			if fragment != "" {
				h.CurrentToolCall.ArgumentsJSON = append(h.CurrentToolCall.ArgumentsJSON, fragment)

				events = append(events, h.buildEvent("response.function_call_arguments.delta", map[string]interface{}{
					"item_id":      h.CurrentToolCall.ID,
					"output_index": h.CurrentToolCall.OutputIndex,
					"delta":        fragment,
				}))
			}
		}

		// 结束工具调用
		if isStop && h.CurrentToolCall != nil {
			fullArgs := strings.Join(h.CurrentToolCall.ArgumentsJSON, "")

			events = append(events, h.buildEvent("response.function_call_arguments.done", map[string]interface{}{
				"item_id":      h.CurrentToolCall.ID,
				"output_index": h.CurrentToolCall.OutputIndex,
				"arguments":    fullArgs,
			}))

			events = append(events, h.buildEvent("response.output_item.done", map[string]interface{}{
				"output_index": h.CurrentToolCall.OutputIndex,
				"item": map[string]interface{}{
					"type":      "function_call",
					"id":        h.CurrentToolCall.ID,
					"call_id":   h.CurrentToolCall.CallID,
					"name":      h.CurrentToolCall.Name,
					"arguments": fullArgs,
					"status":    "completed",
				},
			}))

			h.ToolCalls = append(h.ToolCalls, responsesToolCallInfo{
				ID:        h.CurrentToolCall.ID,
				CallID:    h.CurrentToolCall.CallID,
				Name:      h.CurrentToolCall.Name,
				Arguments: fullArgs,
			})

			h.CurrentToolCall = nil
		}

	case "codeReferenceEvent":
		if toolUses, ok := payload["toolUses"].([]interface{}); ok {
			for _, tu := range toolUses {
				toolUse, ok := tu.(map[string]interface{})
				if !ok {
					continue
				}
				toolCallID, _ := toolUse["toolUseId"].(string)
				toolName, _ := toolUse["name"].(string)
				toolInput, _ := toolUse["input"].(map[string]interface{})

				if toolCallID == "" || toolName == "" {
					continue
				}

				if h.TextStarted {
					events = append(events, h.finalizeTextContent()...)
				}

				h.OutputIndex++
				fcID := "fc_" + toolCallID
				inputJSON, _ := json.Marshal(toolInput)

				events = append(events, h.buildEvent("response.output_item.added", map[string]interface{}{
					"output_index": h.OutputIndex,
					"item": map[string]interface{}{
						"type":      "function_call",
						"id":        fcID,
						"call_id":   toolCallID,
						"name":      toolName,
						"arguments": string(inputJSON),
						"status":    "completed",
					},
				}))

				events = append(events, h.buildEvent("response.output_item.done", map[string]interface{}{
					"output_index": h.OutputIndex,
					"item": map[string]interface{}{
						"type":      "function_call",
						"id":        fcID,
						"call_id":   toolCallID,
						"name":      toolName,
						"arguments": string(inputJSON),
						"status":    "completed",
					},
				}))

				h.ToolCalls = append(h.ToolCalls, responsesToolCallInfo{
					ID:        fcID,
					CallID:    toolCallID,
					Name:      toolName,
					Arguments: string(inputJSON),
				})
			}
		}

	case "assistantResponseEnd":
		// flush 残留的 Kiro 替换缓冲
		if h.PendingKiroBuffer != "" {
			h.ResponseBuffer = append(h.ResponseBuffer, h.PendingKiroBuffer)
			events = append(events, h.buildEvent("response.output_text.delta", map[string]interface{}{
				"item_id":       h.MessageID,
				"output_index":  0,
				"content_index": 0,
				"delta":         h.PendingKiroBuffer,
			}))
			h.PendingKiroBuffer = ""
		}

		if h.TextStarted {
			events = append(events, h.finalizeTextContent()...)
		}
	}

	return events
}

// finalizeTextContent 结束文本内容块，发送 done 事件
func (h *ResponsesStreamHandler) finalizeTextContent() []string {
	if !h.TextStarted {
		return nil
	}
	h.TextStarted = false

	var events []string

	// flush 残留缓冲
	if h.PendingKiroBuffer != "" {
		h.ResponseBuffer = append(h.ResponseBuffer, h.PendingKiroBuffer)
		events = append(events, h.buildEvent("response.output_text.delta", map[string]interface{}{
			"item_id":       h.MessageID,
			"output_index":  0,
			"content_index": 0,
			"delta":         h.PendingKiroBuffer,
		}))
		h.PendingKiroBuffer = ""
	}

	fullText := strings.Join(h.ResponseBuffer, "")

	events = append(events, h.buildEvent("response.output_text.done", map[string]interface{}{
		"item_id":       h.MessageID,
		"output_index":  0,
		"content_index": 0,
		"text":          fullText,
	}))

	events = append(events, h.buildEvent("response.content_part.done", map[string]interface{}{
		"item_id":       h.MessageID,
		"output_index":  0,
		"content_index": 0,
		"part": map[string]interface{}{
			"type":        "output_text",
			"text":        fullText,
			"annotations": []interface{}{},
		},
	}))

	events = append(events, h.buildEvent("response.output_item.done", map[string]interface{}{
		"output_index": 0,
		"item": map[string]interface{}{
			"type":   "message",
			"id":     h.MessageID,
			"status": "completed",
			"role":   "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []interface{}{},
				},
			},
		},
	}))

	return events
}

// BuildCompletedEvent 构建 response.completed 事件（含 usage）
func (h *ResponsesStreamHandler) BuildCompletedEvent(inputTokens, outputTokens int) string {
	var output []interface{}

	fullText := strings.Join(h.ResponseBuffer, "")
	if fullText != "" {
		output = append(output, map[string]interface{}{
			"type":   "message",
			"id":     h.MessageID,
			"status": "completed",
			"role":   "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []interface{}{},
				},
			},
		})
	}

	for _, tc := range h.ToolCalls {
		output = append(output, map[string]interface{}{
			"type":      "function_call",
			"id":        tc.ID,
			"call_id":   tc.CallID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
			"status":    "completed",
		})
	}

	usage := map[string]interface{}{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  inputTokens + outputTokens,
	}

	return h.buildEvent("response.completed", map[string]interface{}{
		"response": h.buildResponseObject("completed", output, usage),
	})
}

// OutputTokens 返回基于流式事件的输出 token 数（乘以2倍系数）
func (h *ResponsesStreamHandler) OutputTokens() int {
	var tokens int
	if h.OutputDeltaCount > 0 {
		tokens = h.OutputDeltaCount
	} else {
		responseText := strings.Join(h.ResponseBuffer, "")
		tokens = len(responseText) / 4
	}
	return tokens * 2
}

// ResponseText 返回已累计的响应文本
func (h *ResponsesStreamHandler) ResponseText() string {
	return strings.Join(h.ResponseBuffer, "")
}

// filterThinking 过滤流式内容中的 <thinking>...</thinking> 块
// 返回 thinking 块之外的文本内容，thinking 内容被丢弃
func (h *ResponsesStreamHandler) filterThinking(content string) string {
	h.ThinkBuffer += content
	var result string

	for h.ThinkBuffer != "" {
		if h.InThinking {
			endIdx := strings.Index(h.ThinkBuffer, ThinkingEndTag)
			if endIdx == -1 {
				// 还在 thinking 中，检查末尾是否有部分结束标签
				pending := pendingTagSuffix(h.ThinkBuffer, ThinkingEndTag)
				if pending > 0 {
					h.ThinkBuffer = h.ThinkBuffer[len(h.ThinkBuffer)-pending:]
				} else {
					h.ThinkBuffer = ""
				}
				return result
			}
			h.InThinking = false
			h.ThinkBuffer = h.ThinkBuffer[endIdx+len(ThinkingEndTag):]
			continue
		}

		startIdx := strings.Index(h.ThinkBuffer, ThinkingStartTag)
		if startIdx == -1 {
			// 无 thinking 标签，检查末尾部分匹配
			pending := pendingTagSuffix(h.ThinkBuffer, ThinkingStartTag)
			if pending > 0 {
				result += h.ThinkBuffer[:len(h.ThinkBuffer)-pending]
				h.ThinkBuffer = h.ThinkBuffer[len(h.ThinkBuffer)-pending:]
			} else {
				result += h.ThinkBuffer
				h.ThinkBuffer = ""
			}
			return result
		}

		result += h.ThinkBuffer[:startIdx]
		h.ThinkBuffer = h.ThinkBuffer[startIdx+len(ThinkingStartTag):]
		h.InThinking = true
	}

	return result
}

// StripThinkingTags 从完整文本中移除所有 <thinking>...</thinking> 块（用于非流式响应）
func StripThinkingTags(text string) string {
	for {
		start := strings.Index(text, ThinkingStartTag)
		if start == -1 {
			return strings.TrimSpace(text)
		}
		end := strings.Index(text[start:], ThinkingEndTag)
		if end == -1 {
			// 没有闭合标签，移除从 start 到末尾
			return strings.TrimSpace(text[:start])
		}
		text = text[:start] + text[start+end+len(ThinkingEndTag):]
	}
}
