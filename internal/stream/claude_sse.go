package stream

import (
	"claude-api/internal/tokenizer"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// thinking 块标签常量
const (
	ThinkingStartTag = "<thinking>"
	ThinkingEndTag   = "</thinking>"
	// thinkingEndTagPlusNewlines </thinking>\n\n 的总长度，用作流式 buffer 末尾保留长度
	thinkingEndTagPlusNewlines = len(ThinkingEndTag) + 2 // 13
)

// quoteCharSet 与 kiro.rs QUOTE_CHARS 对齐
// 当 thinking 标签被这些字符包裹时，认为是被引用而非真正的标签（28 个字符）
var quoteCharSet = func() [256]bool {
	var s [256]bool
	for _, c := range []byte{
		'`', '"', '\'', '\\', '#', '!', '@', '$', '%', '^', '&', '*',
		'(', ')', '-', '_', '=', '+', '[', ']', '{', '}', ';', ':',
		'<', '>', ',', '.', '?', '/',
	} {
		s[c] = true
	}
	return s
}()

// isQuoteChar 判定 buffer 第 pos 字节是否是引用字符（越界返回 false）
func isQuoteChar(buffer string, pos int) bool {
	if pos < 0 || pos >= len(buffer) {
		return false
	}
	return quoteCharSet[buffer[pos]]
}

// findCharBoundary 找到 ≤ target 的最近 UTF-8 字符边界，避免切到多字节字符中间
// 与 kiro.rs find_char_boundary 对齐
func findCharBoundary(s string, target int) int {
	if target >= len(s) {
		return len(s)
	}
	if target <= 0 {
		return 0
	}
	pos := target
	// UTF-8 续字节为 10xxxxxx（0x80~0xBF），向前回退到首字节
	for pos > 0 && (s[pos]&0xC0) == 0x80 {
		pos--
	}
	return pos
}

// findRealThinkingStartTag 找到不被引用字符包裹的 <thinking> 起始位置；找不到返回 -1
func findRealThinkingStartTag(buffer string) int {
	const tag = ThinkingStartTag
	searchStart := 0
	for {
		idx := strings.Index(buffer[searchStart:], tag)
		if idx == -1 {
			return -1
		}
		absPos := searchStart + idx
		if absPos > 0 && isQuoteChar(buffer, absPos-1) {
			searchStart = absPos + 1
			continue
		}
		if isQuoteChar(buffer, absPos+len(tag)) {
			searchStart = absPos + 1
			continue
		}
		return absPos
	}
}

// findRealThinkingEndTag 找到不被引用字符包裹、且后面紧跟 \n\n 的 </thinking> 起始位置
// 流式语境严格要求 \n\n 后缀，避免把 thinking 内容里提到的 </thinking> 字符串误判为结束标签
// 找不到 / 等待更多内容返回 -1
func findRealThinkingEndTag(buffer string) int {
	const tag = ThinkingEndTag
	searchStart := 0
	for {
		idx := strings.Index(buffer[searchStart:], tag)
		if idx == -1 {
			return -1
		}
		absPos := searchStart + idx
		if absPos > 0 && isQuoteChar(buffer, absPos-1) {
			searchStart = absPos + 1
			continue
		}
		afterPos := absPos + len(tag)
		if isQuoteChar(buffer, afterPos) {
			searchStart = absPos + 1
			continue
		}
		afterContent := buffer[afterPos:]
		// \n\n 还没到达，等待下一个 chunk
		if len(afterContent) < 2 {
			return -1
		}
		if strings.HasPrefix(afterContent, "\n\n") {
			return absPos
		}
		searchStart = absPos + 1
	}
}

// findRealThinkingEndTagAtBufferEnd 找到 buffer 末尾的 </thinking>（标签后全是空白字符）
// 用于"边界事件"场景：thinking 后立刻进入 tool_use、流结束等，此时可能没有 \n\n
// 仅当标签后剩余内容全是空白时才认定，避免把 thinking 内容里的 </thinking> 误判
func findRealThinkingEndTagAtBufferEnd(buffer string) int {
	const tag = ThinkingEndTag
	searchStart := 0
	for {
		idx := strings.Index(buffer[searchStart:], tag)
		if idx == -1 {
			return -1
		}
		absPos := searchStart + idx
		if absPos > 0 && isQuoteChar(buffer, absPos-1) {
			searchStart = absPos + 1
			continue
		}
		afterPos := absPos + len(tag)
		if isQuoteChar(buffer, afterPos) {
			searchStart = absPos + 1
			continue
		}
		if strings.TrimSpace(buffer[afterPos:]) == "" {
			return absPos
		}
		searchStart = absPos + 1
	}
}

// ReplaceBrandingWithModel 对文本做品牌名和模型名替换（忽略大小写）
// 把 Kiro 与上游可能返回的 Claude 模型名一律替换为下游请求时传入的 modelName
// 例如下游请求 "claude-opus-4-7"，则 "Kiro"/"Claude Sonnet 4.5" 都会被替换为 "claude-opus-4-7"
func ReplaceBrandingWithModel(text, modelName string) string {
	return replaceBrandingWith(text, modelName, modelName)
}

func replaceBrandingWith(text, kiroReplacement, modelReplacement string) string {
	text = kiroPattern.ReplaceAllString(text, kiroReplacement)
	text = modelWithParenPattern.ReplaceAllString(text, modelReplacement)
	text = modelFriendlyPattern.ReplaceAllString(text, modelReplacement)
	text = modelIDPattern.ReplaceAllString(text, modelReplacement)
	text = chatgptPattern.ReplaceAllString(text, modelReplacement)
	text = gptIDPattern.ReplaceAllString(text, modelReplacement)
	return text
}

// 品牌名和模型名正则（预编译，忽略大小写）
var (
	// 版本号子模式：禁止吞尾部的孤立句号，例如避免 "4.5." 被整体吃掉。
	// 写法：一个或多个数字，可选后接 ".数字" 的组（至少一位数字，杜绝悬空 '.'）。
	verNum = `[\d]+(?:\.[\d]+)*`
	// 模型 ID 尾缀子模式：禁止吞尾部孤立句号。用于 `-xxx` / `-xxx.yyy` 这类 ID 段。
	idSeg = `[\w]+(?:\.[\w]+)*`

	// Kiro（忽略大小写）
	kiroPattern = regexp.MustCompile(`(?i)kiro`)
	// 匹配带括号的完整格式：Claude 3.5 Sonnet (claude-3-5-sonnet-20241022)
	modelWithParenPattern = regexp.MustCompile(`(?i)claude\s+` + verNum + `\s+\w+\s*\(claude-[\w.-]+\)`)
	// 匹配友好名格式：Claude Sonnet、Claude 3.5 Sonnet、Claude Opus 4 等
	// 尾部可选版本号 (?:\s+...)? 确保：
	//   1. "Claude Sonnet 4.5" 整体匹配（不留下多余的 " 4.5"）
	//   2. "Claude Opus 4" 替换为自身（幂等，不会叠加出 "4 4 4"）
	//   3. "Claude Sonnet 4.5." 不把句号也吃掉（verNum 禁止悬空点）
	modelFriendlyPattern = regexp.MustCompile(`(?i)claude\s+(?:` + verNum + `\s+)?(?:sonnet|opus|haiku)(?:\s+` + verNum + `)?`)
	// 匹配纯模型 ID：claude-3-5-sonnet-20241022、claude-sonnet-4-5-20250929 等
	modelIDPattern = regexp.MustCompile(`(?i)claude-(?:` + verNum + `-)*(?:sonnet|opus|haiku)(?:-` + idSeg + `)*`)
	// 匹配 ChatGPT 友好名：ChatGPT、ChatGPT 5、ChatGPT-5、ChatGPT 5.4 等
	chatgptPattern = regexp.MustCompile(`(?i)chatgpt(?:[\s-]*` + verNum + `)?`)
	// 匹配 gpt 形式的模型 ID：gpt-5、gpt-5.4、gpt-5-codex、gpt-5.4-thinking 等
	gptIDPattern = regexp.MustCompile(`(?i)gpt-` + verNum + `(?:-` + idSeg + `)*`)
)

// replaceBrandInContent 在前100个字符范围内做品牌名和模型名替换
// 智能检测尾部是否可能是品牌模式前缀，仅在必要时缓冲（而非固定窗口）
// charsSoFar 是已输出的字符数，content 是新的文本块，pending 是上次保留的尾部，modelName 是下游请求模型名
// 返回：可输出的内容、更新后的字符计数、新的 pending 缓冲
func replaceBrandInContent(content string, charsSoFar int, pending string, modelName string) (string, int, string) {
	return replaceInContent(content, charsSoFar, pending, func(s string) string {
		return ReplaceBrandingWithModel(s, modelName)
	})
}

func replaceInContent(content string, charsSoFar int, pending string, replacer func(string) string) (string, int, string) {
	const replaceLimit = 100

	// 已超过上限，不再替换，flush pending 并直接透传
	if charsSoFar >= replaceLimit {
		if pending != "" {
			content = pending + content
			pending = ""
		}
		return content, charsSoFar + len(content), ""
	}

	// 合并 pending 和新 chunk
	buffer := pending + content

	// 只对前 replaceLimit 范围内的部分做替换
	remaining := replaceLimit - charsSoFar
	var toProcess, passThrough string
	if remaining >= len(buffer) {
		toProcess = buffer
		passThrough = ""
	} else {
		toProcess = buffer[:remaining]
		passThrough = buffer[remaining:]
	}

	// 替换
	replaced := replacer(toProcess)

	if passThrough == "" {
		// 还在范围内：智能检测尾部是否需要缓冲
		pendingLen := calcBrandPendingLen(replaced)
		if pendingLen > 0 && pendingLen < len(replaced) {
			emit := replaced[:len(replaced)-pendingLen]
			return emit, charsSoFar + len(emit), replaced[len(replaced)-pendingLen:]
		}
		if pendingLen >= len(replaced) {
			// 整段都是潜在模式前缀，缓冲等下一个 chunk
			return "", charsSoFar, replaced
		}
		// 尾部无模式前缀，全部输出
		return replaced, charsSoFar + len(replaced), ""
	}

	// 跨越上限边界：对 toProcess 部分做了替换，passThrough 部分直接透传
	return replaced + passThrough, charsSoFar + len(replaced) + len(passThrough), ""
}

// calcBrandPendingLen 从尾部扫描，判断有多少字符可能是品牌模式的前缀
// 所有品牌模式都以 "claude"/"kiro"/"chatgpt"/"gpt-"（忽略大小写）开头
// 只在尾部确实是"仍可能增长"的不完整模式时才需要缓冲，否则返回 0（立即输出）。
//
// 关键规则：如果候选串里已经遇到了无法延伸模式的字符（如 ','、'!'、换行等），
// 说明该位置的 brand 匹配已经闭合，后续文字是普通内容，不应整段缓冲。
func calcBrandPendingLen(text string) int {
	if len(text) == 0 {
		return 0
	}
	lower := strings.ToLower(text)
	maxScan := len(lower)
	if maxScan > 50 {
		maxScan = 50
	}

	for i := 1; i <= maxScan; i++ {
		pos := len(lower) - i
		ch := lower[pos]
		candidate := lower[pos:]

		// "kiro" 不完整前缀（"k","ki","kir","kiro"）
		if ch == 'k' && strings.HasPrefix("kiro", candidate) {
			return i
		}

		if ch == 'c' {
			// 不完整的 "claude" 前缀（"c","cl","cla","clau","claud","claude"）
			if strings.HasPrefix("claude", candidate) {
				return i
			}
			// 不完整的 "chatgpt" 前缀
			if strings.HasPrefix("chatgpt", candidate) {
				return i
			}

			// 已经包含完整 "claude" 但后面还有字符：判断模式是否已经闭合
			if strings.HasPrefix(candidate, "claude") && len(candidate) > 6 {
				nextCh := candidate[6]
				// "claude-...": modelIDPattern 连续字符为 [-\w.]
				if nextCh == '-' {
					if brandPatternClosed(candidate[6:], isIDContChar) {
						continue // 已闭合，不缓冲此位置
					}
					return i
				}
				// "claude ...": modelFriendlyPattern 允许空格/字母/数字/点
				if nextCh == ' ' {
					if brandPatternClosed(candidate[6:], isFriendlyContChar) {
						continue
					}
					return i
				}
				// 其他字符（"claude!", "claude,", "claude."）不可能是模式，跳过
				continue
			}

			// 已经包含完整 "chatgpt"：同理判断是否闭合
			if strings.HasPrefix(candidate, "chatgpt") && len(candidate) > 7 {
				if brandPatternClosed(candidate[7:], isFriendlyContChar) {
					continue
				}
				return i
			}
		}

		if ch == 'g' {
			// 不完整 "gpt-" 前缀: "g","gp","gpt","gpt-"
			if strings.HasPrefix("gpt-", candidate) {
				return i
			}
			// "gpt-" 后面还有字符，判断是否闭合
			if strings.HasPrefix(candidate, "gpt-") && len(candidate) > 4 {
				if brandPatternClosed(candidate[4:], isIDContChar) {
					continue
				}
				return i
			}
		}
	}
	return 0
}

// brandPatternClosed 扫描 suffix，若遇到非延续字符（终结符）返回 true，表示 brand 匹配已闭合。
// 若整个 suffix 都是延续字符则返回 false（仍可能增长，需缓冲）。
func brandPatternClosed(suffix string, isCont func(byte) bool) bool {
	for i := 0; i < len(suffix); i++ {
		if !isCont(suffix[i]) {
			return true
		}
	}
	return false
}

// isIDContChar 判断字符是否可以继续出现在 "claude-..." / "gpt-..." 型 model ID 里。
// 对应正则 [-\w.]，即 a-z, 0-9, _, -, .
func isIDContChar(c byte) bool {
	return c == '-' || c == '.' || c == '_' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z')
}

// isFriendlyContChar 判断字符是否可以继续出现在 "claude sonnet" / "ChatGPT 5" 型友好名里。
// 允许空格、字母、数字、点、短横线。
func isFriendlyContChar(c byte) bool {
	return c == ' ' || c == '.' || c == '-' || c == '_' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z')
}

// SSE 事件格式化
func sseFormat(eventType string, data interface{}) string {
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))
}

// InflateTokenCount 把 token 数按 +5% 上浮（仅用于客户端响应中的 input/output token 字段）
// 内部 c.Set / 日志 / 数据库统计保留原始值，仅在写入下游响应体时调用本函数
// 整数四舍五入（half-up），与 1.05 倍浮点结果保持一致；非正值原样返回避免 0 被放大成 1
func InflateTokenCount(t int) int {
	if t <= 0 {
		return t
	}
	return (t*21 + 10) / 20
}

// BuildMessageStart 构建 message_start 事件
// cacheTokens 可选：[0]=cache_creation_input_tokens, [1]=cache_read_input_tokens
// input_tokens 在写入响应前 +5% 上浮；cache_*_input_tokens 是独立计费维度，保持原值
func BuildMessageStart(conversationID, model string, inputTokens int, cacheTokens ...int) string {
	usage := map[string]int{"input_tokens": InflateTokenCount(inputTokens), "output_tokens": 0}
	if len(cacheTokens) >= 2 {
		usage["cache_creation_input_tokens"] = cacheTokens[0]
		usage["cache_read_input_tokens"] = cacheTokens[1]
	}
	data := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            conversationID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         usage,
		},
	}
	return sseFormat("message_start", data)
}

// BuildContentBlockStart 构建 content_block_start 事件
func BuildContentBlockStart(index int, blockType string) string {
	contentBlock := map[string]interface{}{"type": blockType}
	if blockType == "text" {
		contentBlock["text"] = ""
	} else if blockType == "thinking" {
		contentBlock["thinking"] = ""
	}
	data := map[string]interface{}{
		"type":          "content_block_start",
		"index":         index,
		"content_block": contentBlock,
	}
	return sseFormat("content_block_start", data)
}

// BuildContentBlockDelta 构建 content_block_delta 事件
func BuildContentBlockDelta(index int, text string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{"type": "text_delta", "text": text},
	}
	return sseFormat("content_block_delta", data)
}

// BuildThinkingBlockDelta 构建 thinking_delta 事件
func BuildThinkingBlockDelta(index int, thinking string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{"type": "thinking_delta", "thinking": thinking},
	}
	return sseFormat("content_block_delta", data)
}

// BuildContentBlockStop 构建 content_block_stop 事件
func BuildContentBlockStop(index int) string {
	data := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	return sseFormat("content_block_stop", data)
}

// BuildPing 构建 ping 事件
func BuildPing() string {
	return sseFormat("ping", map[string]string{"type": "ping"})
}

// BuildMessageStop 构建 message_delta 和 message_stop 事件
// output_tokens 在写入响应前 +5% 上浮
func BuildMessageStop(inputTokens, outputTokens int, stopReason string) string {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	deltaData := map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": InflateTokenCount(outputTokens)},
	}
	stopData := map[string]interface{}{"type": "message_stop"}
	return sseFormat("message_delta", deltaData) + sseFormat("message_stop", stopData)
}

// BuildToolUseStart 构建工具调用开始事件
func BuildToolUseStart(index int, toolUseID, toolName string) string {
	data := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    toolUseID,
			"name":  toolName,
			"input": map[string]interface{}{},
		},
	}
	return sseFormat("content_block_start", data)
}

// BuildToolUseInputDelta 构建工具输入增量事件
func BuildToolUseInputDelta(index int, inputJSONDelta string) string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{
			"type":         "input_json_delta",
			"partial_json": inputJSONDelta,
		},
	}
	return sseFormat("content_block_delta", data)
}

// pendingTagSuffix 检查缓冲区末尾是否有标签的部分匹配
func pendingTagSuffix(buffer string, tag string) int {
	if buffer == "" || tag == "" {
		return 0
	}
	maxLen := len(buffer)
	if maxLen > len(tag)-1 {
		maxLen = len(tag) - 1
	}
	for length := maxLen; length > 0; length-- {
		if buffer[len(buffer)-length:] == tag[:length] {
			return length
		}
	}
	return 0
}

// ClaudeStreamHandler 处理 Amazon Q 事件并转换为 Claude SSE 格式
// 用状态管理器确保事件序列符合 Claude 规范
type ClaudeStreamHandler struct {
	Model                 string
	InputTokens           int
	ResponseBuffer        []string
	ContentBlockIndex     int
	ContentBlockStarted   bool
	ContentBlockStartSent bool
	ContentBlockStopSent  bool
	MessageStartSent      bool
	ConversationID        string
	CurrentToolUse        map[string]interface{}
	ToolInputBuffer       []string
	ProcessedToolUseIDs   map[string]bool
	AllToolInputs         []string
	// thinking 块状态
	InThinkBlock                bool
	ThinkBuffer                 string
	StripThinkingLeadingNewline bool // <thinking> 后紧跟的 \n 需被剥离（可能跨 chunk 到达）
	// token 计数（基于流式 delta 事件，更准确）
	// 根据 anthropic-tokenizer 项目，每个流式 delta 对应一个 token
	OutputDeltaCount int
	// 响应结束标志，防止后续事件处理
	ResponseEnded       bool
	CreditUsage         float64
	ContextUsagePercent float64
	// 状态管理器（用于验证事件序列）
	stateManager *SSEStateManager
	// 累计内容字符数，用于前100字符品牌名/模型名替换
	ContentCharCount   int
	PendingKiroBuffer  string
	// 缓存 token 信息（本地计算）
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// NewClaudeStreamHandler 创建 Claude 流处理器
func NewClaudeStreamHandler(model string, inputTokens int) *ClaudeStreamHandler {
	return &ClaudeStreamHandler{
		Model:               model,
		InputTokens:         inputTokens,
		ContentBlockIndex:   -1,
		ProcessedToolUseIDs: make(map[string]bool),
		stateManager:        NewSSEStateManager(false), // 非严格模式
	}
}

// HandleEvent 处理单个 Amazon Q 事件并返回 Claude SSE 事件
func (h *ClaudeStreamHandler) HandleEvent(eventType string, payload map[string]interface{}) []string {
	var events []string

	// 如果响应已结束，提前返回（与 Python 版本保持一致）
	if h.ResponseEnded {
		return events
	}

	switch eventType {
	case "initial-response":
		if !h.MessageStartSent {
			// 直接使用初始化时生成的随机 ID，不从 AWS 响应获取（AWS 返回的可能为空或重复）
			events = append(events, BuildMessageStart(h.ConversationID, h.Model, h.InputTokens, h.CacheCreationInputTokens, h.CacheReadInputTokens))
			h.MessageStartSent = true
			events = append(events, BuildPing())
		}

	case "assistantResponseEvent":
		content, _ := payload["content"].(string)

		// 关闭任何打开的工具调用块（与 Python 版本保持一致，不重置 ContentBlockStartSent）
		if h.CurrentToolUse != nil && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.CurrentToolUse = nil
		}

		// 处理带有 thinking 标签检测的内容
		if content != "" {
			// 前100个字符内做品牌名和模型名替换
			content, h.ContentCharCount, h.PendingKiroBuffer = replaceBrandInContent(content, h.ContentCharCount, h.PendingKiroBuffer, h.Model)
			// 使用 tokenizer 计算实际 token 数，而不是简单 +1
			h.OutputDeltaCount += tokenizer.CountTokens(content)
			h.ThinkBuffer += content
			thinkEvents := h.processThinkBuffer()
			events = append(events, thinkEvents...)
		}

	case "toolUseEvent":
		toolUseID, _ := payload["toolUseId"].(string)
		toolName, _ := payload["name"].(string)
		toolInput := payload["input"]
		isStop, _ := payload["stop"].(bool)

		// 与 kiro2api 一致：处理独立的 input/stop 事件（没有 name 和 toolUseId）
		// AWS EventStream 分片传输时可能出现：
		// 1. {"name":"xxx","toolUseId":"xxx"} - 开始
		// 2. {"input":"..."} - input 数据（没有 name 和 toolUseId）
		// 3. {"stop":true} - 结束（没有 name 和 toolUseId）
		if toolUseID == "" && toolName == "" {
			hasInput := toolInput != nil
			hasStop := isStop

			// 完全无效的事件，直接跳过
			if !hasInput && !hasStop {
				break
			}

			// 如果有当前工具调用，继续处理独立事件
			if h.CurrentToolUse != nil {
				// 累积独立的 input 事件
				if hasInput {
					var fragment string
					switch v := toolInput.(type) {
					case string:
						fragment = v
					default:
						b, _ := json.Marshal(v)
						fragment = string(b)
					}
					if fragment != "" {
						h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
						events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
					}
				}

				// 处理独立的 stop 事件
				if hasStop {
					fullInput := ""
					for _, s := range h.ToolInputBuffer {
						fullInput += s
					}
					h.AllToolInputs = append(h.AllToolInputs, fullInput)

					events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
					h.ContentBlockStopSent = true
					h.ContentBlockStarted = false
					h.ContentBlockStartSent = false
					h.CurrentToolUse = nil
					h.ToolInputBuffer = nil
				}
			}
			// 独立事件处理完毕，跳出
			break
		}

		// 开始新的工具调用（有 toolUseId 和 name）
		if toolUseID != "" && toolName != "" && h.CurrentToolUse == nil {
			// 检查是否已处理过此工具调用ID（去重）
			if h.ProcessedToolUseIDs[toolUseID] {
				break
			}

			// 边界事件：thinking 直接进入 tool_use 时，</thinking> 后可能没有 \n\n
			// 此时需要按"buffer 末尾全空白"的规则刷出 thinking 块，避免遗留
			events = append(events, h.flushThinkBufferAtEnd()...)

			// 关闭之前的文本块
			if h.ContentBlockStartSent && !h.ContentBlockStopSent {
				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStartSent = false
			}

			h.ProcessedToolUseIDs[toolUseID] = true
			h.ContentBlockIndex++

			events = append(events, BuildToolUseStart(h.ContentBlockIndex, toolUseID, toolName))

			h.CurrentToolUse = map[string]interface{}{"toolUseId": toolUseID, "name": toolName}
			h.ToolInputBuffer = nil
			h.ContentBlockStopSent = false
			h.ContentBlockStartSent = true
			h.ContentBlockStarted = true

			// 如果开始事件同时包含 input，立即处理
			if toolInput != nil {
				var fragment string
				switch v := toolInput.(type) {
				case string:
					fragment = v
				default:
					b, _ := json.Marshal(v)
					fragment = string(b)
				}
				if fragment != "" {
					h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
					events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
				}
			}

			// 如果开始事件同时包含 stop，立即结束（一次性完整数据）
			if isStop {
				fullInput := ""
				for _, s := range h.ToolInputBuffer {
					fullInput += s
				}
				h.AllToolInputs = append(h.AllToolInputs, fullInput)

				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStarted = false
				h.ContentBlockStartSent = false
				h.CurrentToolUse = nil
				h.ToolInputBuffer = nil
			}
			break
		}

		// 累积输入（后续的带 toolUseId 的 input 事件）
		if h.CurrentToolUse != nil && toolInput != nil {
			var fragment string
			switch v := toolInput.(type) {
			case string:
				fragment = v
			default:
				b, _ := json.Marshal(v)
				fragment = string(b)
			}
			if fragment != "" {
				h.ToolInputBuffer = append(h.ToolInputBuffer, fragment)
				events = append(events, BuildToolUseInputDelta(h.ContentBlockIndex, fragment))
			}
		}

		// 停止工具调用（后续的带 toolUseId 的 stop 事件）
		if isStop && h.CurrentToolUse != nil {
			fullInput := ""
			for _, s := range h.ToolInputBuffer {
				fullInput += s
			}
			h.AllToolInputs = append(h.AllToolInputs, fullInput)

			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.ContentBlockStarted = false
			h.ContentBlockStartSent = false
			h.CurrentToolUse = nil
			h.ToolInputBuffer = nil
		}

	case "assistantResponseEnd":
		// flush 残留的 Kiro 替换缓冲（pending 已经过 ReplaceBranding 处理，直接输出）
		if h.PendingKiroBuffer != "" {
			h.ThinkBuffer += h.PendingKiroBuffer
			h.PendingKiroBuffer = ""
			flushEvents := h.processThinkBuffer()
			events = append(events, flushEvents...)
		}

		// 关闭任何打开的块
		if h.ContentBlockStarted && !h.ContentBlockStopSent {
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
		}

		// 标记响应已结束，防止后续事件处理（与 Python 版本保持一致）
		h.ResponseEnded = true

		// 立即发送 message_stop（与 Python 版本保持一致，不等待 Finish()）
		outputTokens := h.OutputDeltaCount
		if outputTokens < 1 {
			fullText := strings.Join(h.ResponseBuffer, "")
			fullToolInput := strings.Join(h.AllToolInputs, "")
			outputTokens = tokenizer.CountTokens(fullText + fullToolInput)
			if outputTokens < 1 {
				outputTokens = 1
			}
		}
		events = append(events, BuildMessageStop(h.InputTokens, outputTokens, h.stopReason()))

	case "meteringEvent":
		// 提取 credit usage
		if creditUsage, ok := payload["creditUsage"].(float64); ok {
			h.CreditUsage = creditUsage
		} else if usage, ok := payload["usage"].(float64); ok {
			h.CreditUsage = usage
		}
		// metering 事件不需要发送到客户端

	case "contextUsageEvent":
		// 提取 context usage percentage
		if percent, ok := payload["contextUsagePercentage"].(float64); ok {
			h.ContextUsagePercent = percent
		}
		// context usage 事件不需要发送到客户端
	}

	return events
}

// processThinkBuffer 处理 think 缓冲区，检测 thinking 标签
// 与 kiro.rs process_content_with_thinking 对齐：
//   - 28 字符引用前后缀过滤，避免把内容里提到的 <thinking>/</thinking> 误判
//   - </thinking> 严格要求后跟 \n\n（流式语境）
//   - <thinking> 前的纯空白前缀不发出，避免在 thinking 块前生成空 text 块
//   - <thinking> 后紧跟的 \n 跨 chunk 剥离（StripThinkingLeadingNewline 标志位）
//   - 找不到 end tag 时保留末尾 13 字节（</thinking>\n\n 长度）防止部分标签被错误发出
//   - findCharBoundary 保证 UTF-8 多字节字符不被切断
func (h *ClaudeStreamHandler) processThinkBuffer() []string {
	var events []string

	for h.ThinkBuffer != "" {
		if !h.InThinkBlock {
			startPos := findRealThinkingStartTag(h.ThinkBuffer)
			if startPos == -1 {
				// 没找到 <thinking>：保留末尾 len(<thinking>)=10 字节防止跨 chunk 部分标签
				target := len(h.ThinkBuffer) - len(ThinkingStartTag)
				if target < 0 {
					target = 0
				}
				safeLen := findCharBoundary(h.ThinkBuffer, target)
				if safeLen > 0 {
					safeContent := h.ThinkBuffer[:safeLen]
					// 纯空白前缀且 thinking 还没出现 → 暂不发出，留在 buffer 等更多内容
					// 避免在 thinking 块前先创建空 text 块导致客户端解析错乱
					if strings.TrimSpace(safeContent) != "" {
						events = append(events, h.emitTextDelta(safeContent)...)
						h.ThinkBuffer = h.ThinkBuffer[safeLen:]
					}
				}
				break
			}

			// 找到 <thinking>：发送之前的内容（跳过纯空白前缀）
			beforeText := h.ThinkBuffer[:startPos]
			if strings.TrimSpace(beforeText) != "" {
				events = append(events, h.emitTextDelta(beforeText)...)
			}
			h.ThinkBuffer = h.ThinkBuffer[startPos+len(ThinkingStartTag):]

			// 关闭可能存在的 text 块
			if h.ContentBlockStartSent && !h.ContentBlockStopSent {
				events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
				h.ContentBlockStopSent = true
				h.ContentBlockStartSent = false
			}

			// 开 thinking 块
			h.ContentBlockIndex++
			events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "thinking"))
			h.ContentBlockStartSent = true
			h.ContentBlockStarted = true
			h.ContentBlockStopSent = false
			h.InThinkBlock = true
			h.StripThinkingLeadingNewline = true
			continue
		}

		// 在 thinking 块内
		// 剥离 <thinking>\n 中的前导 \n（可能跨 chunk）
		if h.StripThinkingLeadingNewline {
			if strings.HasPrefix(h.ThinkBuffer, "\n") {
				h.ThinkBuffer = h.ThinkBuffer[1:]
				h.StripThinkingLeadingNewline = false
			} else if h.ThinkBuffer != "" {
				h.StripThinkingLeadingNewline = false
			}
			// buffer 为空则保留标志，等下一个 chunk
			if h.ThinkBuffer == "" {
				break
			}
		}

		endPos := findRealThinkingEndTag(h.ThinkBuffer)
		if endPos != -1 {
			// 提取 thinking 内容
			thinkingChunk := h.ThinkBuffer[:endPos]
			if thinkingChunk != "" {
				events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, thinkingChunk))
			}

			// 关闭 thinking 块
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.ContentBlockStartSent = false
			h.InThinkBlock = false

			// 跳过 </thinking>\n\n（findRealThinkingEndTag 已确认 \n\n 存在）
			h.ThinkBuffer = h.ThinkBuffer[endPos+thinkingEndTagPlusNewlines:]
			continue
		}

		// 没找到 </thinking>\n\n：保留末尾 13 字节（</thinking>\n\n 总长）防止部分标签被发出
		target := len(h.ThinkBuffer) - thinkingEndTagPlusNewlines
		if target < 0 {
			target = 0
		}
		safeLen := findCharBoundary(h.ThinkBuffer, target)
		if safeLen > 0 {
			thinkingChunk := h.ThinkBuffer[:safeLen]
			if thinkingChunk != "" {
				events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, thinkingChunk))
			}
			h.ThinkBuffer = h.ThinkBuffer[safeLen:]
		}
		break
	}

	return events
}

// emitTextDelta 把内容作为 text_delta 发出，必要时先创建 text 块
func (h *ClaudeStreamHandler) emitTextDelta(text string) []string {
	var events []string
	if !h.ContentBlockStartSent {
		h.ContentBlockIndex++
		events = append(events, BuildContentBlockStart(h.ContentBlockIndex, "text"))
		h.ContentBlockStartSent = true
		h.ContentBlockStarted = true
		h.ContentBlockStopSent = false
	}
	h.ResponseBuffer = append(h.ResponseBuffer, text)
	events = append(events, BuildContentBlockDelta(h.ContentBlockIndex, text))
	return events
}

// flushThinkBufferAtEnd 在流结束 / 边界事件（如 thinking 直接进入 tool_use）时刷出剩余 buffer
// 接受 </thinking> 后只有空白字符的情况（findRealThinkingEndTagAtBufferEnd），
// 避免流尾遗漏的 thinking 块没有正确结束
func (h *ClaudeStreamHandler) flushThinkBufferAtEnd() []string {
	var events []string
	if h.ThinkBuffer == "" {
		return events
	}

	if h.InThinkBlock {
		// 优先严格匹配（带 \n\n）
		endPos := findRealThinkingEndTag(h.ThinkBuffer)
		if endPos == -1 {
			// 退而求其次：标签后只有空白
			endPos = findRealThinkingEndTagAtBufferEnd(h.ThinkBuffer)
		}
		if endPos != -1 {
			thinkingChunk := h.ThinkBuffer[:endPos]
			if thinkingChunk != "" {
				events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, thinkingChunk))
			}
			events = append(events, BuildContentBlockStop(h.ContentBlockIndex))
			h.ContentBlockStopSent = true
			h.ContentBlockStartSent = false
			h.InThinkBlock = false
			h.ThinkBuffer = ""
		} else {
			// 没有结束标签，把剩余作为 thinking_delta 发出（最后兜底）
			events = append(events, BuildThinkingBlockDelta(h.ContentBlockIndex, h.ThinkBuffer))
			h.ThinkBuffer = ""
		}
		return events
	}

	// 不在 thinking 块内：剩余 buffer 作为 text 发出
	if strings.TrimSpace(h.ThinkBuffer) != "" {
		events = append(events, h.emitTextDelta(h.ThinkBuffer)...)
	}
	h.ThinkBuffer = ""
	return events
}

// Finish 返回最终的 SSE 事件
func (h *ClaudeStreamHandler) Finish() string {
	// 如果响应已结束（message_stop 已在 assistantResponseEnd 中发送），跳过
	if h.ResponseEnded {
		return ""
	}

	var result string

	// 流尾刷出 think 缓冲区里残留的内容（接受 </thinking> 后只有空白的边界场景）
	for _, ev := range h.flushThinkBufferAtEnd() {
		result += ev
	}

	// 确保最后一个块已关闭
	if h.ContentBlockStarted && !h.ContentBlockStopSent {
		result += BuildContentBlockStop(h.ContentBlockIndex)
		h.ContentBlockStopSent = true
	}

	// 使用流式 delta 计数作为输出 token 数
	// 根据 anthropic-tokenizer 项目，每个流式事件对应一个 token，这是最准确的计数方式
	outputTokens := h.OutputDeltaCount
	if outputTokens < 1 {
		// 如果没有 delta 事件，回退到文本估算（使用 Claude tokenizer）
		fullText := strings.Join(h.ResponseBuffer, "")
		fullToolInput := strings.Join(h.AllToolInputs, "")
		outputTokens = tokenizer.CountTokens(fullText + fullToolInput)
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	result += BuildMessageStop(h.InputTokens, outputTokens, h.stopReason())
	return result
}

func (h *ClaudeStreamHandler) stopReason() string {
	if len(h.ProcessedToolUseIDs) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

// OutputTokens 返回基于流式事件的输出 token 数
// @author ygw
func (h *ClaudeStreamHandler) OutputTokens() int {
	var tokens int
	if h.OutputDeltaCount > 0 {
		tokens = h.OutputDeltaCount
	} else {
		// 回退到文本估算（使用 Claude tokenizer）
		fullText := strings.Join(h.ResponseBuffer, "")
		fullToolInput := strings.Join(h.AllToolInputs, "")
		tokens = tokenizer.CountTokens(fullText + fullToolInput)
		if tokens < 1 {
			tokens = 1
		}
	}
	return tokens
}

// ResponseText 返回已累计的回答文本
func (h *ClaudeStreamHandler) ResponseText() string {
	return strings.Join(h.ResponseBuffer, "")
}

// Error 构造错误事件（Claude 格式）
func (h *ClaudeStreamHandler) Error(msg string) string {
	data := map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": msg,
		},
	}
	return sseFormat("error", data)
}
