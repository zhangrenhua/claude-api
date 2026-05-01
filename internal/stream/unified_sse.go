package stream

import (
	"encoding/json"
	"fmt"
	"claude-api/internal/tokenizer"
	"strings"
)

// 统一 SSE 事件构造
func buildUnifiedEvent(eventType string, data interface{}) string {
	payload, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(payload))
}

// UnifiedStreamHandler 将 Amazon Q 事件转换为统一的 SSE 协议
// 支持双通道输出：thinking_delta 与 answer_delta
type UnifiedStreamHandler struct {
	Model          string
	ConversationID string
	ResponseID     string
	InputTokens    int

	// 内部状态
	outputBuffer                []string
	thinkingBuffer              string
	inThinking                  bool
	metaSent                    bool
	stripThinkingLeadingNewline bool // <thinking>\n 中前导 \n 跨 chunk 剥离
	doneSent                    bool
	// token 计数（基于流式 delta 事件，更准确）
	// 根据 anthropic-tokenizer 项目，每个流式 delta 对应一个 token
	outputDeltaCount int
	// 累计内容字符数，用于前100字符品牌名/模型名替换
	contentCharCount  int
	pendingKiroBuffer string
	// 缓存 token 信息（本地计算）
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

func NewUnifiedStreamHandler(model string, conversationID string, inputTokens int) *UnifiedStreamHandler {
	return &UnifiedStreamHandler{
		Model:          model,
		ConversationID: conversationID,
		InputTokens:    inputTokens,
		outputBuffer:   []string{},
	}
}

// HandleEvent 将 AWS EventStream 事件映射为统一 SSE 事件
func (h *UnifiedStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	switch eventType {
	case "initial-response":
		if !h.metaSent {
			// 直接使用初始化时生成的随机 ID，不从 AWS 响应获取（AWS 返回的可能为空或重复）
			meta := map[string]interface{}{
				"type":            "meta",
				"conversation_id": h.ConversationID,
				"model":           h.Model,
				"input_tokens":    InflateTokenCount(h.InputTokens),
			}
			if h.CacheCreationInputTokens > 0 {
				meta["cache_creation_input_tokens"] = h.CacheCreationInputTokens
			}
			if h.CacheReadInputTokens > 0 {
				meta["cache_read_input_tokens"] = h.CacheReadInputTokens
			}
			events = append(events, buildUnifiedEvent("meta", meta))
			h.metaSent = true
		}

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)
		if content != "" {
			// 前100个字符内做品牌名和模型名替换
			content, h.contentCharCount, h.pendingKiroBuffer = replaceBrandInContent(content, h.contentCharCount, h.pendingKiroBuffer, h.Model)
			// 使用 tokenizer 计算实际 token 数，而不是简单 +1
			h.outputDeltaCount += tokenizer.CountTokens(content)
			h.thinkingBuffer += content
			events = append(events, h.flushThinkingBuffer()...)
		}

	case "assistantResponseEnd":
		// flush 残留的 Kiro 替换缓冲（pending 已经过 ReplaceBranding 处理，直接输出）
		if h.pendingKiroBuffer != "" {
			h.thinkingBuffer += h.pendingKiroBuffer
			h.pendingKiroBuffer = ""
			events = append(events, h.flushThinkingBuffer()...)
		}

		// 流尾刷出 thinkingBuffer 中保留的尾部字节（最多 10/13 字节）
		// 用 buffer-end 边界规则识别 </thinking>（标签后只有空白即可）
		events = append(events, h.flushThinkingBufferAtEnd()...)

		if !h.doneSent {
			events = append(events, h.done("stop"))
		}
	}

	return events
}

// done 完成流时输出 done 事件
func (h *UnifiedStreamHandler) done(reason string) string {
	if h.doneSent {
		return ""
	}
	h.doneSent = true

	// 使用流式 delta 计数作为输出 token 数
	// 根据 anthropic-tokenizer 项目，每个流式事件对应一个 token
	outputTokens := h.outputDeltaCount
	if outputTokens < 1 {
		// 如果没有 delta 事件，回退到文本估算（使用 Claude tokenizer）
		outputText := strings.Join(h.outputBuffer, "")
		outputTokens = tokenizer.CountTokens(outputText)
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	return buildUnifiedEvent("done", map[string]interface{}{
		"type":          "done",
		"finish_reason": reason,
		"usage": map[string]int{
			"input_tokens":  InflateTokenCount(h.InputTokens),
			"output_tokens": InflateTokenCount(outputTokens),
		},
	})
}

// OutputTokens 返回基于流式事件的输出 token 数
// @author ygw
func (h *UnifiedStreamHandler) OutputTokens() int {
	var tokens int
	if h.outputDeltaCount > 0 {
		tokens = h.outputDeltaCount
	} else {
		// 回退到文本估算（使用 Claude tokenizer）
		outputText := strings.Join(h.outputBuffer, "")
		tokens = tokenizer.CountTokens(outputText)
		if tokens < 1 {
			tokens = 1
		}
	}
	return tokens
}

// Error 构造错误事件
func (h *UnifiedStreamHandler) Error(msg string) string {
	return buildUnifiedEvent("error", map[string]interface{}{
		"type":    "error",
		"message": msg,
	})
}

// ResponseText 返回已累计的回答文本
func (h *UnifiedStreamHandler) ResponseText() string {
	return strings.Join(h.outputBuffer, "")
}

// Finish 输出最终 done 事件
func (h *UnifiedStreamHandler) Finish(reason string) string {
	return h.done(reason)
}

// flushThinkingBuffer 解析 thinking 标签，输出对应事件
// 与 claude_sse.go 共用 findRealThinking* 系列：28 字符引用前后缀过滤、</thinking> 后强制 \n\n
// 跨 chunk 部分标签保护：起始保留末尾 10 字节，结束保留末尾 13 字节
func (h *UnifiedStreamHandler) flushThinkingBuffer() []string {
	var events []string

	for h.thinkingBuffer != "" {
		if !h.inThinking {
			startPos := findRealThinkingStartTag(h.thinkingBuffer)
			if startPos == -1 {
				// 没找到起始：保留末尾 10 字节防部分标签
				target := len(h.thinkingBuffer) - len(ThinkingStartTag)
				if target < 0 {
					target = 0
				}
				safeLen := findCharBoundary(h.thinkingBuffer, target)
				if safeLen > 0 {
					textChunk := h.thinkingBuffer[:safeLen]
					if strings.TrimSpace(textChunk) != "" {
						h.outputBuffer = append(h.outputBuffer, textChunk)
						events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
							"type": "answer_delta",
							"text": textChunk,
						}))
						h.thinkingBuffer = h.thinkingBuffer[safeLen:]
					}
				}
				break
			}

			before := h.thinkingBuffer[:startPos]
			// 跳过纯空白前缀，避免在 thinking 块前生成空 answer_delta
			if strings.TrimSpace(before) != "" {
				h.outputBuffer = append(h.outputBuffer, before)
				events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
					"type": "answer_delta",
					"text": before,
				}))
			}
			h.thinkingBuffer = h.thinkingBuffer[startPos+len(ThinkingStartTag):]
			h.inThinking = true
			h.stripThinkingLeadingNewline = true
			continue
		}

		// 在 thinking 块内：剥离 <thinking>\n 的前导 \n
		if h.stripThinkingLeadingNewline {
			if strings.HasPrefix(h.thinkingBuffer, "\n") {
				h.thinkingBuffer = h.thinkingBuffer[1:]
				h.stripThinkingLeadingNewline = false
			} else if h.thinkingBuffer != "" {
				h.stripThinkingLeadingNewline = false
			}
			if h.thinkingBuffer == "" {
				break
			}
		}

		endPos := findRealThinkingEndTag(h.thinkingBuffer)
		if endPos != -1 {
			thinkChunk := h.thinkingBuffer[:endPos]
			if thinkChunk != "" {
				events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
					"type": "thinking_delta",
					"text": thinkChunk,
				}))
			}
			h.thinkingBuffer = h.thinkingBuffer[endPos+thinkingEndTagPlusNewlines:]
			h.inThinking = false
			continue
		}

		// 没找到 </thinking>\n\n：保留末尾 13 字节
		target := len(h.thinkingBuffer) - thinkingEndTagPlusNewlines
		if target < 0 {
			target = 0
		}
		safeLen := findCharBoundary(h.thinkingBuffer, target)
		if safeLen > 0 {
			thinkChunk := h.thinkingBuffer[:safeLen]
			if thinkChunk != "" {
				events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
					"type": "thinking_delta",
					"text": thinkChunk,
				}))
			}
			h.thinkingBuffer = h.thinkingBuffer[safeLen:]
		}
		break
	}

	return events
}

// flushThinkingBufferAtEnd 流尾刷出 thinkingBuffer 中残留的尾部字节
// 与 flushThinkingBuffer 配合：常规流程保留 10/13 字节防部分标签，流结束时本函数释放尾部
// 在 thinking 块内：用 buffer-end 边界规则（标签后只有空白即可）识别结束标签
// 不在 thinking 块内：剩余 buffer 全部作为 answer_delta 发出
func (h *UnifiedStreamHandler) flushThinkingBufferAtEnd() []string {
	var events []string
	if h.thinkingBuffer == "" {
		return events
	}

	for h.thinkingBuffer != "" {
		if h.inThinking {
			endPos := findRealThinkingEndTag(h.thinkingBuffer)
			if endPos == -1 {
				endPos = findRealThinkingEndTagAtBufferEnd(h.thinkingBuffer)
			}
			if endPos != -1 {
				thinkChunk := h.thinkingBuffer[:endPos]
				if thinkChunk != "" {
					events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
						"type": "thinking_delta",
						"text": thinkChunk,
					}))
				}
				h.thinkingBuffer = h.thinkingBuffer[endPos+len(ThinkingEndTag):]
				h.thinkingBuffer = strings.TrimLeft(h.thinkingBuffer, " \t\r\n")
				h.inThinking = false
				continue
			}
			// 没找到结束标签：剩余作为 thinking_delta 兜底发出
			events = append(events, buildUnifiedEvent("thinking_delta", map[string]interface{}{
				"type": "thinking_delta",
				"text": h.thinkingBuffer,
			}))
			h.thinkingBuffer = ""
			break
		}

		// 非 thinking 状态：剩余 buffer 作为 answer 发出
		startPos := findRealThinkingStartTag(h.thinkingBuffer)
		if startPos == -1 {
			if strings.TrimSpace(h.thinkingBuffer) != "" {
				h.outputBuffer = append(h.outputBuffer, h.thinkingBuffer)
				events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
					"type": "answer_delta",
					"text": h.thinkingBuffer,
				}))
			}
			h.thinkingBuffer = ""
			break
		}
		// 末尾还有 <thinking> 起始：发出之前的内容，进入 thinking 模式
		before := h.thinkingBuffer[:startPos]
		if strings.TrimSpace(before) != "" {
			h.outputBuffer = append(h.outputBuffer, before)
			events = append(events, buildUnifiedEvent("answer_delta", map[string]interface{}{
				"type": "answer_delta",
				"text": before,
			}))
		}
		h.thinkingBuffer = h.thinkingBuffer[startPos+len(ThinkingStartTag):]
		h.inThinking = true
	}
	return events
}
