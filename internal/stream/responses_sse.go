package stream

import (
	"claude-api/internal/tokenizer"
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
	AllToolInputs     []string
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

		// 按原始内容累计 token（含 thinking，因为上游实际消耗了这些 token）
		h.OutputDeltaCount += tokenizer.CountTokens(content)

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
		content, h.ContentCharCount, h.PendingKiroBuffer = replaceBrandInContent(content, h.ContentCharCount, h.PendingKiroBuffer, h.Model)

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

				// 工具名也算 output token（上游吐出的内容）
				h.OutputDeltaCount += tokenizer.CountTokens(toolName)

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
				h.AllToolInputs = append(h.AllToolInputs, fragment)
				h.OutputDeltaCount += tokenizer.CountTokens(fragment)

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

				// codeReferenceEvent 一次性给出完整 tool_use，整块计入 output token
				h.AllToolInputs = append(h.AllToolInputs, string(inputJSON))
				h.OutputDeltaCount += tokenizer.CountTokens(toolName + string(inputJSON))

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

		// 流尾刷出 ThinkBuffer 中保留的尾部字节（最多 10/13 字节）
		// 处理 buffer-end 边界：thinking 后只有空白也认作结束标签
		if tail := h.flushFilterThinkingAtEnd(); tail != "" {
			h.ResponseBuffer = append(h.ResponseBuffer, tail)
			events = append(events, h.buildEvent("response.output_text.delta", map[string]interface{}{
				"item_id":       h.MessageID,
				"output_index":  0,
				"content_index": 0,
				"delta":         tail,
			}))
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

	inflatedIn := InflateTokenCount(inputTokens)
	inflatedOut := InflateTokenCount(outputTokens)
	usage := map[string]interface{}{
		"input_tokens":  inflatedIn,
		"output_tokens": inflatedOut,
		"total_tokens":  inflatedIn + inflatedOut,
	}

	return h.buildEvent("response.completed", map[string]interface{}{
		"response": h.buildResponseObject("completed", output, usage),
	})
}

// OutputTokens 返回基于流式事件的输出 token 数
// 纯工具调用、纯 thinking 场景也能正确计数
func (h *ResponsesStreamHandler) OutputTokens() int {
	if h.OutputDeltaCount > 0 {
		return h.OutputDeltaCount
	}
	// 回退到完整文本估算（含工具输入）
	fullText := strings.Join(h.ResponseBuffer, "")
	fullToolInput := strings.Join(h.AllToolInputs, "")
	tokens := tokenizer.CountTokens(fullText + fullToolInput)
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// ResponseText 返回已累计的响应文本
func (h *ResponsesStreamHandler) ResponseText() string {
	return strings.Join(h.ResponseBuffer, "")
}

// filterThinking 过滤流式内容中的 <thinking>...</thinking> 块
// 返回 thinking 块之外的文本内容，thinking 内容被丢弃
// 与 claude_sse.go 共用 findRealThinking* 系列：28 字符引用前后缀过滤、</thinking> 后强制 \n\n
func (h *ResponsesStreamHandler) filterThinking(content string) string {
	h.ThinkBuffer += content
	var result string

	for h.ThinkBuffer != "" {
		if h.InThinking {
			endPos := findRealThinkingEndTag(h.ThinkBuffer)
			if endPos == -1 {
				// 还在 thinking 中：保留末尾 13 字节防止部分 </thinking>\n\n 被错误丢弃
				target := len(h.ThinkBuffer) - thinkingEndTagPlusNewlines
				if target < 0 {
					target = 0
				}
				safeLen := findCharBoundary(h.ThinkBuffer, target)
				h.ThinkBuffer = h.ThinkBuffer[safeLen:]
				return result
			}
			h.InThinking = false
			h.ThinkBuffer = h.ThinkBuffer[endPos+thinkingEndTagPlusNewlines:]
			continue
		}

		startPos := findRealThinkingStartTag(h.ThinkBuffer)
		if startPos == -1 {
			// 无 thinking 起始标签：保留末尾 len(<thinking>)=10 字节防止部分标签被发出
			target := len(h.ThinkBuffer) - len(ThinkingStartTag)
			if target < 0 {
				target = 0
			}
			safeLen := findCharBoundary(h.ThinkBuffer, target)
			result += h.ThinkBuffer[:safeLen]
			h.ThinkBuffer = h.ThinkBuffer[safeLen:]
			return result
		}

		result += h.ThinkBuffer[:startPos]
		h.ThinkBuffer = h.ThinkBuffer[startPos+len(ThinkingStartTag):]
		h.InThinking = true
	}

	return result
}

// flushFilterThinkingAtEnd 流尾刷出 ThinkBuffer 中残留的字节，返回非 thinking 内容
// 与 filterThinking 配合使用：filterThinking 在常规流程中保留末尾 10/13 字节防部分标签
// 流结束时调用本函数释放尾部，并用 buffer-end 边界规则识别 </thinking>（标签后只有空白）
// thinking 内容仍然被丢弃（与 filterThinking 语义一致）
func (h *ResponsesStreamHandler) flushFilterThinkingAtEnd() string {
	if h.ThinkBuffer == "" {
		return ""
	}
	var result string

	for h.ThinkBuffer != "" {
		if h.InThinking {
			// 优先严格匹配（带 \n\n）
			endPos := findRealThinkingEndTag(h.ThinkBuffer)
			if endPos == -1 {
				// 退而求其次：标签后只有空白
				endPos = findRealThinkingEndTagAtBufferEnd(h.ThinkBuffer)
			}
			if endPos == -1 {
				// 没有结束标签：剩余视为 thinking 内容直接丢弃
				h.ThinkBuffer = ""
				break
			}
			h.InThinking = false
			h.ThinkBuffer = h.ThinkBuffer[endPos+len(ThinkingEndTag):]
			h.ThinkBuffer = strings.TrimLeft(h.ThinkBuffer, " \t\r\n")
			continue
		}

		// 非 thinking 状态：剩余 buffer 全部作为文本输出
		startPos := findRealThinkingStartTag(h.ThinkBuffer)
		if startPos == -1 {
			result += h.ThinkBuffer
			h.ThinkBuffer = ""
			break
		}
		result += h.ThinkBuffer[:startPos]
		h.ThinkBuffer = h.ThinkBuffer[startPos+len(ThinkingStartTag):]
		h.InThinking = true
	}
	return result
}

// StripThinkingTags 从完整文本中移除所有 <thinking>...</thinking> 块（用于非流式响应）
// 与流式判定一致：起始用 findRealThinkingStartTag，结束优先 \n\n 严格匹配，退化到末尾全空白
func StripThinkingTags(text string) string {
	for {
		startPos := findRealThinkingStartTag(text)
		if startPos == -1 {
			return strings.TrimSpace(text)
		}
		afterOpen := text[startPos+len(ThinkingStartTag):]
		if endPos := findRealThinkingEndTag(afterOpen); endPos != -1 {
			text = text[:startPos] + afterOpen[endPos+thinkingEndTagPlusNewlines:]
			continue
		}
		if endPos := findRealThinkingEndTagAtBufferEnd(afterOpen); endPos != -1 {
			text = text[:startPos] + strings.TrimLeft(afterOpen[endPos+len(ThinkingEndTag):], " \t\r\n")
			continue
		}
		// 没有闭合标签，移除从 start 到末尾
		return strings.TrimSpace(text[:startPos])
	}
}

// SplitContentWithThinking 把含 <thinking>...</thinking>\n\n 块的非流式文本拆成 Claude 格式 content 数组
// 输出顺序保持原文中的 thinking / text 交替顺序；多块支持
//   - thinking 块 → {"type":"thinking","thinking":"..."}
//   - 其余文本   → {"type":"text","text":"..."}
// 与流式 processThinkBuffer 的判定逻辑一致：28 字符引用过滤 + </thinking> 后强制 \n\n（流尾允许空白）
// 没有 thinking 标签的输入会原样返回单 text 块；纯空白片段会被跳过
func SplitContentWithThinking(text string) []map[string]interface{} {
	var blocks []map[string]interface{}
	remaining := text

	for remaining != "" {
		startPos := findRealThinkingStartTag(remaining)
		if startPos == -1 {
			if strings.TrimSpace(remaining) != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": remaining})
			}
			return blocks
		}

		// 起始标签前的文本块（跳过纯空白）
		before := remaining[:startPos]
		if strings.TrimSpace(before) != "" {
			blocks = append(blocks, map[string]interface{}{"type": "text", "text": before})
		}

		afterOpen := remaining[startPos+len(ThinkingStartTag):]
		var thinkingContent, textAfter string
		if endPos := findRealThinkingEndTag(afterOpen); endPos != -1 {
			thinkingContent = afterOpen[:endPos]
			textAfter = afterOpen[endPos+thinkingEndTagPlusNewlines:]
		} else if endPos := findRealThinkingEndTagAtBufferEnd(afterOpen); endPos != -1 {
			thinkingContent = afterOpen[:endPos]
			textAfter = strings.TrimLeft(afterOpen[endPos+len(ThinkingEndTag):], " \t\r\n")
		} else {
			// 没有有效的结束标签：起始标签开始的部分一并作为 text 输出
			if strings.TrimSpace(remaining[startPos:]) != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": remaining[startPos:]})
			}
			return blocks
		}

		// 剥离 thinking 内容前导 \n（与流式行为一致）
		thinkingContent = strings.TrimPrefix(thinkingContent, "\n")
		if thinkingContent != "" {
			blocks = append(blocks, map[string]interface{}{"type": "thinking", "thinking": thinkingContent})
		}

		remaining = textAfter
	}
	return blocks
}
