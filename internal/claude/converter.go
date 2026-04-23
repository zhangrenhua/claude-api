package claude

import (
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"claude-api/internal/utils"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// getOperatingSystem 获取当前操作系统名称（Amazon Q 格式）
func getOperatingSystem() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// thinking 模式相关常量
// 注意：ThinkingHint 只需要一次，与 Python 参考项目 (claude-api) 保持一致
const (
	ThinkingHint     = "<thinking_mode>interleaved</thinking_mode><max_thinking_length>16000</max_thinking_length>"
	ThinkingStartTag = "<thinking>"
	ThinkingEndTag   = "</thinking>"
)

// 有效的模型名称集合
var validModels = map[string]bool{
	"auto":              true,
	"claude-sonnet-4":   true,
	"claude-sonnet-4.5": true,
	"claude-haiku-4.5":  true,
	"claude-opus-4.5":   true,
}

// 规范模型名称到短名称的映射
var canonicalToShort = map[string]string{
	"claude-sonnet-4-20250514":   "claude-sonnet-4",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4.5",
	"claude-sonnet-4-5":          "claude-sonnet-4.5",
	"claude-haiku-4-5-20251001":  "claude-haiku-4.5",
	"claude-opus-4-5-20251101":   "claude-opus-4.5",
	// Claude 3.5 Sonnet 旧版映射
	"claude-3-5-sonnet-20241022": "claude-sonnet-4.5",
	"claude-3-5-sonnet-20240620": "claude-sonnet-4.5",
}

// MapModelName 将 Claude 模型名称映射到 Amazon Q 模型 ID
// 支持短名称（如 claude-sonnet-4）和规范名称（如 claude-sonnet-4-20250514）
func MapModelName(claudeModel string) string {
	const defaultModel = "claude-sonnet-4.5"

	modelLower := strings.ToLower(claudeModel)

	// 检查是否是有效的短名称
	if validModels[modelLower] {
		return modelLower
	}

	// 检查是否是规范名称
	if shortName, ok := canonicalToShort[modelLower]; ok {
		return shortName
	}

	// 未知模型，返回默认模型
	return defaultModel
}

// downstreamToUpstreamModel 下游请求模型名 → 上游期望模型名 的映射表
// key 一律小写；查询时输入也会先小写化（见 MapDownstreamModel）
var downstreamToUpstreamModel = map[string]string{
	"claud 4.6":                  "claude-sonnet-4-5-20250929",
	"claude opus 4.6":            "claude-sonnet-4-5-20250929",
	"claude-2.0":                 "claude-2.0",
	"claude-3-5-haiku-20241022":  "claude-3-5-haiku-20241022",
	"claude-3-5-sonnet-20241022": "claude-haiku-4-5-20251001",
	"claude-3-7-sonnet-20250219": "claude-haiku-4-5-20251001",
	"claude-3-haiku-20240307":    "claude-3-haiku-20240307",
	"claude-3-sonnet-20240229":   "claude-3-sonnet-20240229",
	"claude-haiku-4-20250514":    "claude-haiku-4-20250514",
	"claude-haiku-4-5":           "claude-haiku-4-5-20251001",
	"claude-haiku-4-5-2025100":   "claude-haiku-4-5-2025100",
	"claude-haiku-4-5-20251001":  "claude-haiku-4-5-20251001",
	"claude-opus-4-20250514":     "claude-sonnet-4-20250514",
	"claude-opus-4-5":            "claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101":   "claude-sonnet-4-5-20250929",
	"claude-opus-4-6":            "claude-sonnet-4-5-20250929",
	"claude-opus-4-6-20260130":   "claude-sonnet-4-5-20250929",
	"claude-opus-4-6-thinking":   "claude-sonnet-4-5-20250929",
	"claude-opus-4-7":            "claude-sonnet-4-5-20250929",
	"claude-sonnet-4-20250514":   "claude-sonnet-4-20250514",
	"claude-sonnet-4-5":          "claude-sonnet-4-5-20250929",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-5-20250929",
	"claude-sonnet-4-6":          "claude-sonnet-4-5-20250929",
	"claude-sonnet-4-6-20260217": "claude-sonnet-4-5-20250929",
	"gpt-5":                      "claude-sonnet-4-20250514",
	"gpt-5-codex":                "claude-sonnet-4-20250514",
	"gpt-5-codex-mini":           "claude-sonnet-4-20250514",
	"gpt-5.1":                    "claude-sonnet-4-20250514",
	"gpt-5.1-codex":              "claude-sonnet-4-20250514",
	"gpt-5.1-codex-max":          "claude-sonnet-4-20250514",
	"gpt-5.1-codex-mini":         "claude-sonnet-4-20250514",
	"gpt-5.2":                    "claude-sonnet-4-20250514",
	"gpt-5.2-codex":              "claude-sonnet-4-20250514",
	"gpt-5.3-codex":              "claude-sonnet-4-20250514",
	"gpt-5.3-codex-spark":        "claude-sonnet-4-20250514",
	"gpt-5.4":                    "claude-sonnet-4-5-20250929",
	"gpt-5.4-mini":               "claude-sonnet-4-20250514",
	"gpt-5.4-thinking":           "claude-sonnet-4-5-20250929",
}

// MapDownstreamModel 把下游请求模型名翻译为上游模型名
// 大小写不敏感、忽略首尾空白；不在表中返回 ok=false
func MapDownstreamModel(name string) (string, bool) {
	v, ok := downstreamToUpstreamModel[strings.ToLower(strings.TrimSpace(name))]
	return v, ok
}

// getCurrentTimestamp 获取当前时间戳
func getCurrentTimestamp() string {
	now := time.Now()
	return now.Format("Monday, 2006-01-02T15:04:05.000-07:00")
}

// wrapThinkingContent 将 thinking 内容包装成 XML 标签
func wrapThinkingContent(text string) string {
	return ThinkingStartTag + text + ThinkingEndTag
}

// isThinkingModeEnabled 检测是否启用了 thinking 模式
func isThinkingModeEnabled(thinking interface{}) bool {
	if thinking == nil {
		return false
	}
	switch t := thinking.(type) {
	case bool:
		return t
	case string:
		return strings.ToLower(t) == "enabled"
	case map[string]interface{}:
		// 检查 type 字段
		if typeVal, ok := t["type"].(string); ok {
			if strings.ToLower(typeVal) == "enabled" {
				return true
			}
		}
		// 检查 enabled 字段
		if enabled, ok := t["enabled"].(bool); ok {
			return enabled
		}
		// 检查 budget_tokens 字段
		if budget, ok := t["budget_tokens"].(float64); ok && budget > 0 {
			return true
		}
		if budget, ok := t["budget_tokens"].(int); ok && budget > 0 {
			return true
		}
	}
	return false
}

// appendThinkingHint 在文本末尾添加 thinking 提示
func appendThinkingHint(text string) string {
	if text == "" {
		return ThinkingHint
	}
	normalized := strings.TrimRight(text, " \t\r\n")
	if strings.HasSuffix(normalized, ThinkingHint) {
		return text
	}
	separator := ""
	if !strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r") {
		separator = "\n"
	}
	return text + separator + ThinkingHint
}

// detectToolCallLoop 检测是否存在无限工具调用循环
// 只有当同一工具在连续的 assistant 消息中被调用 threshold 次且输入相同时才触发
func detectToolCallLoop(messages []models.ClaudeMessage, threshold int) error {
	if threshold <= 0 {
		threshold = 3
	}

	var lastToolCall struct {
		name  string
		input string
	}
	consecutiveCount := 0

	// 检查最近 10 条消息
	startIdx := len(messages) - 10
	if startIdx < 0 {
		startIdx = 0
	}

	for _, msg := range messages[startIdx:] {
		if msg.Role == "assistant" {
			if blocks, ok := msg.Content.([]interface{}); ok {
				for _, block := range blocks {
					if m, ok := block.(map[string]interface{}); ok {
						if m["type"] == "tool_use" {
							toolName, _ := m["name"].(string)
							toolInput, _ := m["input"].(map[string]interface{})
							inputJSON, _ := json.Marshal(toolInput)
							currentInput := string(inputJSON)

							currentCall := struct {
								name  string
								input string
							}{toolName, currentInput}

							if currentCall.name == lastToolCall.name && currentCall.input == lastToolCall.input {
								consecutiveCount++
							} else {
								consecutiveCount = 1
								lastToolCall = currentCall
							}
						}
					}
				}
			}
		} else if msg.Role == "user" {
			// 用户消息打断连续链
			consecutiveCount = 0
			lastToolCall = struct {
				name  string
				input string
			}{}
		}
	}

	// 只有连续相同调用才触发
	if consecutiveCount >= threshold {
		return fmt.Errorf("检测到无限循环: 工具 '%s' 连续调用 %d 次且输入相同", lastToolCall.name, consecutiveCount)
	}

	return nil
}

// isValidToolUseID 判断 tool_use_id 是否符合上游接受的格式
// 上游（Amazon Q/CodeWhisperer）拒绝含 `:`、`.`、`$` 或以工具名前缀的 ID（如 `Bash:19`、`functions.Read:0`、`$AskUserQuestion`）
// 合法 ID 为字母数字和 `_`/`-` 的组合，不能包含上述特殊字符。
func isValidToolUseID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r == ':' || r == '.' || r == '$' || r == ' ' || r == '/' {
			return false
		}
	}
	return true
}

// generateToolUseID 生成符合上游规范的新 tool_use_id（`toolu_` + 24 位十六进制随机串）
func generateToolUseID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 兜底：使用时间戳；虽不理想但仍符合字符集要求
		return fmt.Sprintf("toolu_%d", time.Now().UnixNano())
	}
	return "toolu_" + hex.EncodeToString(b[:])
}

// MaxToolNameLen 是 Anthropic / Amazon Q 接受的工具名最大长度。
// 来自 Anthropic 官方约束 ^[a-zA-Z0-9_-]{1,64}$，Amazon Q 同样按此校验，
// 超长会返回 ValidationException("Improperly formed request")。
const MaxToolNameLen = 64

// sanitizeToolName 将超长工具名截断到 64 字符以内，使用 SHA-1 前 8 位十六进制作为后缀，
// 保证同一原名经过本函数始终映射为同一短名（确定性，无需共享 map），
// 这样 tools[] 与历史中 toolUses[].name 即便分别处理也保持一致。
// 长度 ≤ MaxToolNameLen 的名称原样返回。
func sanitizeToolName(name string) string {
	if len(name) <= MaxToolNameLen {
		return name
	}
	sum := sha1.Sum([]byte(name))
	suffix := hex.EncodeToString(sum[:4]) // 8 hex 字符
	// 保留前 (MaxToolNameLen - 1 - len(suffix)) = 55 字符 + "_" + 8 字符 = 64
	return name[:MaxToolNameLen-1-len(suffix)] + "_" + suffix
}

// sanitizeToolUseIDs 扫描所有消息，将不合法的 tool_use.id 替换为合法 ID，
// 并同步更新 user 消息中 tool_result.tool_use_id 的引用。
// 这是为修复 Amazon Q 对形如 `Bash:19`、`functions.Read:0`、`$AskUserQuestion` 等 ID 返回
// "Improperly formed request" ValidationException 的问题。
func sanitizeToolUseIDs(messages []models.ClaudeMessage) {
	if len(messages) == 0 {
		return
	}

	// 第一遍：找出所有 assistant tool_use 的不合法 ID，生成映射
	idMap := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		blocks, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for _, block := range blocks {
			m, ok := block.(map[string]interface{})
			if !ok || m["type"] != "tool_use" {
				continue
			}
			id, _ := m["id"].(string)
			if isValidToolUseID(id) {
				continue
			}
			if _, exists := idMap[id]; exists {
				continue
			}
			idMap[id] = generateToolUseID()
		}
	}

	if len(idMap) == 0 {
		return
	}

	// 第二遍：应用映射到 assistant tool_use.id 与 user tool_result.tool_use_id
	for _, msg := range messages {
		blocks, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for _, block := range blocks {
			m, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			switch m["type"] {
			case "tool_use":
				if oldID, _ := m["id"].(string); oldID != "" {
					if newID, ok := idMap[oldID]; ok {
						m["id"] = newID
					}
				}
			case "tool_result":
				if oldID, _ := m["tool_use_id"].(string); oldID != "" {
					if newID, ok := idMap[oldID]; ok {
						m["tool_use_id"] = newID
					}
				}
			}
		}
	}

	logger.Info("[消息修复] 清洗 %d 个非法 tool_use_id，映射到 toolu_ 规范格式", len(idMap))
}

// fixOrphanToolResults 清理孤立的 tool_result
// 当 user 消息中包含 tool_result 但前面没有 assistant 消息包含对应的 tool_use 时，
// 直接移除这些孤立的 tool_result，避免上游拒绝请求
// fixOrphanToolResults 顺序扫描消息，维护"尚未被 tool_result 消费的 tool_use id"集合，
// 移除两类非法 tool_result：
//  1. 从未声明过（availableToolUseIDs 中不存在）——纯孤儿；
//  2. 已被更早的 tool_result 消费过（重复/重放）——CodeWhisperer 也会判为非法。
//
// 客户端（尤其是某些 IDE 插件）可能在重试/状态回放时把已配对过的 tool_result 再发一次，
// 旧实现只按"是否声明过"判定会漏掉这种情况。
func fixOrphanToolResults(messages []models.ClaudeMessage) []models.ClaudeMessage {
	if len(messages) == 0 {
		return messages
	}

	pending := make(map[string]bool) // 待消费的 tool_use id
	removed := 0
	modified := false
	result := make([]models.ClaudeMessage, 0, len(messages))

	for _, msg := range messages {
		if msg.Role == "assistant" {
			if blocks, ok := msg.Content.([]interface{}); ok {
				for _, block := range blocks {
					if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_use" {
						if id, ok := m["id"].(string); ok && id != "" {
							pending[id] = true
						}
					}
				}
			}
			result = append(result, msg)
			continue
		}

		if msg.Role == "user" {
			if blocks, ok := msg.Content.([]interface{}); ok {
				cleaned := make([]interface{}, 0, len(blocks))
				localRemoved := 0
				for _, block := range blocks {
					if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_result" {
						if toolUseID, ok := m["tool_use_id"].(string); ok && toolUseID != "" {
							if pending[toolUseID] {
								delete(pending, toolUseID) // 本次消费
							} else {
								localRemoved++
								continue
							}
						}
					}
					cleaned = append(cleaned, block)
				}
				if localRemoved > 0 {
					modified = true
					removed += localRemoved
					if len(cleaned) == 0 {
						// 整条 user 消息已清空，跳过该消息
						continue
					}
					msg.Content = interface{}(cleaned)
				}
			}
		}
		result = append(result, msg)
	}

	if removed > 0 {
		logger.Info("[消息修复] 移除 %d 个非法 tool_result（孤儿或已被消费）", removed)
	}
	if !modified {
		return messages
	}
	return result
}

// mergeConsecutiveToolResults 合并连续的 tool_result user 消息
// 某些客户端（如 Claude Code）会将同一轮的多个 tool_result 拆成独立的 user 消息，
// 但 Amazon Q 要求 assistant toolUses 后的 user 消息必须包含所有对应的 toolResults。
func mergeConsecutiveToolResults(messages []models.ClaudeMessage) []models.ClaudeMessage {
	if len(messages) < 2 {
		return messages
	}

	result := make([]models.ClaudeMessage, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		if msg.Role == "user" && isAllToolResults(msg.Content) {
			blocks, _ := msg.Content.([]interface{})
			merged := make([]interface{}, len(blocks))
			copy(merged, blocks)

			count := 1
			for i+1 < len(messages) && messages[i+1].Role == "user" && isAllToolResults(messages[i+1].Content) {
				i++
				count++
				nextBlocks, _ := messages[i].Content.([]interface{})
				merged = append(merged, nextBlocks...)
			}

			if count > 1 {
				logger.Info("[消息修复] 合并 %d 个连续的 tool_result user 消息为一条", count)
				msg.Content = interface{}(merged)
			}
		}

		result = append(result, msg)
	}

	return result
}

// mergeConsecutiveSameRoleMessages 合并所有连续同角色消息为一条。
// Anthropic 原生 API 接受相邻同角色消息，但 Amazon Q / CodeWhisperer
// 要求严格的 user/assistant 交替，否则抛 "Improperly formed request"。
// 典型触发场景：某些 IDE 插件（Roo Code / Cline 类）先发 tool_result
// user 消息，再紧跟一条 image+text 的 user 消息描述环境。
//
// 本函数假设 mergeConsecutiveToolResults 已先行执行过。
// content 为字符串时会被包装成 [{type:text,text:<s>}] 再合并。
func mergeConsecutiveSameRoleMessages(messages []models.ClaudeMessage) []models.ClaudeMessage {
	if len(messages) < 2 {
		return messages
	}

	// toBlocks 将 content 规整为 []interface{}。
	// 返回值: (blocks, ok)。ok=false 表示遇到未知类型，调用方必须避免有损合并。
	toBlocks := func(content interface{}) ([]interface{}, bool) {
		switch c := content.(type) {
		case nil:
			return nil, true
		case []interface{}:
			return c, true
		case string:
			if c == "" {
				return nil, true
			}
			return []interface{}{
				map[string]interface{}{"type": "text", "text": c},
			}, true
		}
		return nil, false
	}

	result := make([]models.ClaudeMessage, 0, len(messages))
	merged := 0

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if len(result) == 0 || result[len(result)-1].Role != msg.Role {
			result = append(result, msg)
			continue
		}
		prev := &result[len(result)-1]
		prevBlocks, prevOK := toBlocks(prev.Content)
		curBlocks, curOK := toBlocks(msg.Content)
		if !prevOK || !curOK {
			// 任一侧为未知类型：放弃合并，保持独立（上游会拒绝，但不静默丢数据）
			logger.Warn("[消息修复] 发现未知 content 类型，跳过合并 role=%s", msg.Role)
			result = append(result, msg)
			continue
		}
		combined := make([]interface{}, 0, len(prevBlocks)+len(curBlocks))
		combined = append(combined, prevBlocks...)
		combined = append(combined, curBlocks...)
		prev.Content = interface{}(combined)
		merged++
	}

	if merged > 0 {
		logger.Info("[消息修复] 合并 %d 条连续同角色消息，避免上游拒绝", merged)
	}
	return result
}

// isAllToolResults 检查消息内容是否全部为 tool_result 块
func isAllToolResults(content interface{}) bool {
	blocks, ok := content.([]interface{})
	if !ok || len(blocks) == 0 {
		return false
	}
	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok || m["type"] != "tool_result" {
			return false
		}
	}
	return true
}

// flattenToolReferencesForNoToolsRequest 当请求没有定义任何工具时，
// 将历史和当前消息中的结构化 toolUses/toolResults 转换为纯文本。
// 场景：客户端（如 Zed 编辑器的标题生成）发送请求时不携带工具定义，
// 但对话历史中包含工具调用，上游会因"引用未定义的工具"返回 ValidationException。
// 返回是否做了任何修改。
func flattenToolReferencesForNoToolsRequest(history []models.HistoryMessage, currentMsg *models.UserInputMessage) bool {
	modified := false

	for i := range history {
		// 将 assistant 消息中的 toolUses 转换为文本描述
		if am := history[i].AssistantResponseMessage; am != nil && len(am.ToolUses) > 0 {
			var names []string
			for _, tu := range am.ToolUses {
				names = append(names, tu.Name)
			}
			toolText := "[Called: " + strings.Join(names, ", ") + "]"
			if am.Content == "..." || am.Content == "" {
				am.Content = toolText
			} else {
				am.Content += "\n" + toolText
			}
			am.ToolUses = nil
			modified = true
		}

		// 移除 user 消息中的 toolResults（文本内容已包含足够上下文）
		if um := history[i].UserInputMessage; um != nil && len(um.UserInputMessageContext.ToolResults) > 0 {
			um.UserInputMessageContext.ToolResults = nil
			modified = true
		}
	}

	// 移除当前消息中的 toolResults
	if currentMsg != nil && len(currentMsg.UserInputMessageContext.ToolResults) > 0 {
		currentMsg.UserInputMessageContext.ToolResults = nil
		modified = true
	}

	return modified
}

// fixDanglingToolUses 修复 history 尾部 assistant 发起了 tool_use 但 currentMessage 缺少配对 tool_result 的情况。
// 当用户跳过工具执行直接发送文本消息时会出现此情况。
// 上游要求每个 tool_use 都有配对的 tool_result，否则返回 ValidationException。
// 修复方式：将缺少配对的 toolResult 注入到 currentMessage 的 context 中。
func fixDanglingToolUses(history []models.HistoryMessage, currentMsg *models.UserInputMessage) {
	if len(history) == 0 || currentMsg == nil {
		return
	}

	// 找到 history 最后一条 assistant 消息
	var lastAssistant *models.AssistantResponseMessage
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].AssistantResponseMessage != nil {
			lastAssistant = history[i].AssistantResponseMessage
			break
		}
	}
	if lastAssistant == nil || len(lastAssistant.ToolUses) == 0 {
		return
	}

	// 收集 currentMessage 中已有的 toolResult IDs
	existingResults := make(map[string]bool)
	for _, tr := range currentMsg.UserInputMessageContext.ToolResults {
		existingResults[tr.ToolUseID] = true
	}

	// 为缺少配对的 tool_use 注入合成 toolResult
	var injected int
	for _, tu := range lastAssistant.ToolUses {
		if existingResults[tu.ToolUseID] {
			continue
		}
		currentMsg.UserInputMessageContext.ToolResults = append(
			currentMsg.UserInputMessageContext.ToolResults,
			models.ToolResult{
				ToolUseID: tu.ToolUseID,
				Content: []models.ToolResultContent{
					{Text: "Tool execution was interrupted by the user."},
				},
				Status: "error",
			},
		)
		injected++
	}

	if injected > 0 {
		logger.Info("[消息转换] 注入 %d 个合成 toolResult 以修复悬空 tool_use", injected)
	}
}

func extractSystemText(system interface{}) string {
	switch s := system.(type) {
	case string:
		return s
	case []interface{}:
		var parts []string
		for _, block := range s {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" {
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// determineChatTriggerType 根据过滤后的工具列表判断触发类型
// 注意：始终使用 MANUAL，Amazon Q 对 AUTO 的支持不稳定，
// 且 Anthropic 的 tool_choice 语义与 Amazon Q 的 chatTriggerType 不对等
func determineChatTriggerType(req *models.ClaudeRequest, filteredToolCount int) string {
	return "MANUAL"
}

// ConvertClaudeToAmazonQ 将 Claude 请求转换为 Amazon Q 格式
// 返回 AmazonQRequest 结构体，使用自定义 MarshalJSON 确保字段顺序与参考项目一致
// 如果检测到无限循环，返回 nil 和错误信息
func ConvertClaudeToAmazonQ(req *models.ClaudeRequest, conversationID string, _ bool) (*models.AmazonQRequest, error) {
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// 清洗非法的 tool_use_id（形如 Bash:19、functions.Read:0、$AskUserQuestion），
	// 上游会对这类 ID 返回 "Improperly formed request"。在所有其他修复之前执行。
	sanitizeToolUseIDs(req.Messages)

	// 修复孤立的 tool_result：为缺少对应 tool_use 的 tool_result 补充 assistant 消息
	req.Messages = fixOrphanToolResults(req.Messages)

	// 合并连续的 tool_result user 消息（某些客户端如 Claude Code 会将同一轮的多个 tool_result 拆成独立 user 消息）
	req.Messages = mergeConsecutiveToolResults(req.Messages)

	// 再做一次通用同角色合并，兜底处理 tool_result + 普通文本/图片 user 消息相邻的情况
	// （Roo Code / Cline 类客户端典型模式），否则 CodeWhisperer 会抛 "Improperly formed request"。
	req.Messages = mergeConsecutiveSameRoleMessages(req.Messages)

	// 检测无限工具调用循环
	if err := detectToolCallLoop(req.Messages, 3); err != nil {
		return nil, err
	}

	// 检测 thinking 模式
	thinkingEnabled := isThinkingModeEnabled(req.Thinking)

	// 1. 转换工具
	var aqTools []models.Tool
	for _, t := range req.Tools {
		// 跳过 Anthropic 内置工具（如 web_search、code_execution 等）
		// 这些工具的 input_schema 为 null 且由 Anthropic 服务端处理，Amazon Q 不支持
		if t.InputSchema == nil {
			logger.Debug("[消息转换] 跳过内置工具（无 input_schema）: %s", t.Name)
			continue
		}

		desc := t.Description
		// 处理空描述（Amazon Q 不接受空的 description）
		if desc == "" {
			desc = "..."
		}
		// 限制 description 长度为 10000 字符（与参考项目一致）
		if len(desc) > 10000 {
			desc = desc[:10000]
			logger.Debug("[消息转换] 工具描述超长已截断: %s", t.Name)
		}
		// 递归清理 inputSchema 中 Amazon Q 不接受的字段
		inputSchema := sanitizeJSONSchema(t.InputSchema)

		// 兜底：若清理后为空对象或缺少 type 字段，补齐到最小合法 schema。
		if len(inputSchema) == 0 {
			inputSchema = map[string]interface{}{"type": "object"}
			logger.Debug("[消息转换] 工具 %s 的 input_schema 为空，填充最小合法 schema", t.Name)
		} else if _, hasType := inputSchema["type"]; !hasType {
			inputSchema["type"] = "object"
			logger.Debug("[消息转换] 工具 %s 的 input_schema 缺少 type，补齐为 object", t.Name)
		}

		// CodeWhisperer 对 type=object 且 properties 为空/缺失的工具 schema 会抛
		// "Improperly formed request"（实际观测到大量 ValidationException）。
		// 注入一个占位无参属性，语义与 "无参数" 等价且上游接受。
		if typ, _ := inputSchema["type"].(string); typ == "object" {
			props, _ := inputSchema["properties"].(map[string]interface{})
			if len(props) == 0 {
				inputSchema["properties"] = map[string]interface{}{
					"_noargs": map[string]interface{}{
						"type":        "string",
						"description": "unused placeholder (tool takes no arguments)",
					},
				}
				logger.Debug("[消息转换] 工具 %s 的 properties 为空，注入 _noargs 占位避免上游拒绝", t.Name)
			}
		}

		// 工具名长度规整：>64 字符的名称会被上游拒绝（ValidationException），
		// 需要确定性截断，使 history 中 toolUses[].name 经过同一函数后仍能匹配。
		aqName := sanitizeToolName(t.Name)
		if aqName != t.Name {
			logger.Debug("[消息转换] 工具名超长已截断: %s -> %s", t.Name, aqName)
		}

		aqTools = append(aqTools, models.Tool{
			ToolSpecification: models.ToolSpecification{
				Name:        aqName,
				Description: desc,
				InputSchema: models.ToolInputSchema{
					JSON: inputSchema,
				},
			},
		})
	}

	// 日志记录被过滤的内置工具
	if len(req.Tools) > 0 && len(aqTools) < len(req.Tools) {
		skipped := len(req.Tools) - len(aqTools)
		logger.Info("[消息转换] 已过滤 %d 个上游不支持的内置工具，剩余可用工具: %d", skipped, len(aqTools))
	}

	// 2. 处理最后一条消息
	var promptContent string
	var toolResults []models.ToolResult
	var images []models.AmazonQImage
	hasToolResult := false

	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" {
			promptContent = extractClaudeTextContent(lastMsg.Content)
			toolResults = extractClaudeToolResults(lastMsg.Content)
			images = extractImagesFromContent(lastMsg.Content)
			hasToolResult = len(toolResults) > 0
		}
	}

	// 从上一个 assistant 消息中获取 tool_use 顺序，用于重新排序当前消息的 tool_results
	if len(toolResults) > 0 && len(req.Messages) >= 2 {
		// 查找当前 user 消息之前的最后一个 assistant 消息
		for i := len(req.Messages) - 2; i >= 0; i-- {
			if req.Messages[i].Role == "assistant" {
				lastToolUseOrder := extractToolUseOrder(req.Messages[i].Content)
				if len(lastToolUseOrder) > 0 {
					toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
					logger.Debug("[消息转换] 重新排序 %d 个当前消息 tool_results 以匹配 tool_use 顺序", len(toolResults))
				}
				break
			}
		}
	}

	// 如果启用了 thinking 模式，添加 thinking 提示
	if thinkingEnabled && promptContent != "" {
		promptContent = appendThinkingHint(promptContent)
	}

	// 3. 构建上下文
	userCtx := models.UserInputMessageContext{
		EnvState: models.EnvState{
			OperatingSystem:         getOperatingSystem(),
			CurrentWorkingDirectory: "/",
		},
	}
	if len(aqTools) > 0 {
		userCtx.Tools = aqTools
	}
	if len(toolResults) > 0 {
		userCtx.ToolResults = toolResults
	}

	// 4. 格式化内容
	formattedContent := promptContent
	hasImages := len(images) > 0
	hasTools := len(aqTools) > 0

	if hasToolResult && promptContent == "" {
		// 与参考项目一致：当 content 为空但有 toolResults 时，使用默认提示
		formattedContent = "Tool results provided."
	} else if promptContent == "" && !hasImages && hasTools {
		//  如果没有内容但有工具，注入占位内容
		formattedContent = "执行工具任务"
		logger.Debug("[消息转换] 注入占位内容以触发工具调用 - 工具数: %d", len(aqTools))
	} else if promptContent == "" && !hasImages && !hasToolResult {
		// 与 AIClient-2-API 一致：使用 "Continue" 作为占位符，而不是返回错误
		formattedContent = "Continue"
		logger.Debug("[消息转换] 内容为空，使用默认占位符 'Continue'")
	}

	// 5. 模型映射（支持规范名称和短名称）
	modelID := MapModelName(req.Model)

	// 6. 用户输入消息（
	userInputMsg := models.UserInputMessage{
		Content:                 formattedContent,
		UserInputMessageContext: userCtx,
		Origin:                  "AI_EDITOR", // 与参考项目一致
		ModelID:                 modelID,
		Images:                  images,
	}

	// 7. 构建历史消息
	var historyMsgs []models.ClaudeMessage
	if len(req.Messages) > 1 {
		historyMsgs = req.Messages[:len(req.Messages)-1]
	}

	// 处理 system prompt（与参考项目一致：转换为 user-assistant 消息对）
	var systemHistory []models.HistoryMessage
	if req.System != nil {
		sysText := extractSystemText(req.System)
		if sysText != "" {
			// system prompt 转换为 user-assistant 消息对
			systemHistory = append(systemHistory, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content: sysText,
					UserInputMessageContext: models.UserInputMessageContext{
						EnvState: models.EnvState{
							OperatingSystem:         getOperatingSystem(),
							CurrentWorkingDirectory: "/",
						},
					},
					Origin:  "AI_EDITOR",
					ModelID: modelID,
				},
			})
			systemHistory = append(systemHistory, models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: "OK",
				},
			})
		}
	}

	// 处理常规历史消息
	aqHistory := processClaudeHistoryWithMessageID(historyMsgs, thinkingEnabled, modelID)

	// 合并 system 历史和常规历史
	fullHistory := append(systemHistory, aqHistory...)

	// 后处理：移除与 currentMessage 内容相同的尾部 userInputMessage
	if len(fullHistory) > 0 && formattedContent != "" {
		lastItem := fullHistory[len(fullHistory)-1]
		if lastItem.UserInputMessage != nil {
			lastContent := strings.TrimSpace(lastItem.UserInputMessage.Content)
			currentContent := strings.TrimSpace(formattedContent)
			if lastContent != "" && lastContent == currentContent {
				fullHistory = fullHistory[:len(fullHistory)-1]
				logger.Info("[消息转换] 移除与 currentMessage 内容相同的尾部 userInputMessage，防止重复响应")
			}
		}
	}

	// 当请求没有定义任何工具时，将 toolUses/toolResults 平坦化为纯文本
	// 避免上游因"引用了未定义的工具"返回 ValidationException ("Improperly formed request")
	if len(aqTools) == 0 {
		if flattenToolReferencesForNoToolsRequest(fullHistory, &userInputMsg) {
			logger.Info("[消息转换] 请求无工具定义但包含工具引用，已平坦化为纯文本")
		}
	}

	// 修复悬空 tool_use：history 尾部 assistant 发起了 tool_use，
	// 但 currentMessage 没有提供对应的 tool_result（用户跳过了工具执行直接发文本）。
	// 上游要求 tool_use 必须有配对的 tool_result，否则返回 "Improperly formed request"。
	fixDanglingToolUses(fullHistory, &userInputMsg)

	// 8. 构建最终负载
	result := &models.AmazonQRequest{
		ConversationState: models.ConversationState{
			ConversationID:  conversationID,
			History:         fullHistory,
			CurrentMessage:  models.CurrentMessage{UserInputMessage: userInputMsg},
			ChatTriggerType: determineChatTriggerType(req, len(aqTools)), // 基于过滤后的工具列表判断
		},
	}

	// 调试日志
	logger.Debug("[消息转换] 原始消息数: %d, 历史消息数: %d, chatTriggerType: %s",
		len(req.Messages), len(fullHistory), result.ConversationState.ChatTriggerType)

	return result, nil
}

// countToolUses 统计消息中的 tool_use 数量
func countToolUses(content interface{}) int {
	blocks, ok := content.([]interface{})
	if !ok {
		return 0
	}
	count := 0
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_use" {
			count++
		}
	}
	return count
}

// extractToolUseOrder 从 assistant 消息内容中提取 tool_use ID 的顺序
// 用于在下一个 user 消息中重新排序 tool_results 以匹配
func extractToolUseOrder(content interface{}) []string {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var order []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok && m["type"] == "tool_use" {
			if tid, ok := m["id"].(string); ok && tid != "" {
				order = append(order, tid)
			}
		}
	}
	return order
}

// reorderToolResultsByToolUses 根据 tool_use 顺序重新排序 tool_results
// 这对于防止并行工具调用时结果顺序不一致导致模型混淆至关重要
func reorderToolResultsByToolUses(toolResults []models.ToolResult, toolUseOrder []string) []models.ToolResult {
	if len(toolUseOrder) == 0 || len(toolResults) == 0 {
		return toolResults
	}

	resultByID := make(map[string]models.ToolResult)
	for _, r := range toolResults {
		resultByID[r.ToolUseID] = r
	}

	var orderedResults []models.ToolResult
	// 按 tool_use 顺序添加结果
	for _, toolUseID := range toolUseOrder {
		if r, ok := resultByID[toolUseID]; ok {
			orderedResults = append(orderedResults, r)
			delete(resultByID, toolUseID)
		}
	}

	// 添加不在原始顺序中的剩余结果（正常情况下不应该发生）
	for _, r := range resultByID {
		orderedResults = append(orderedResults, r)
	}

	return orderedResults
}

// mergeToolResultIntoMap 将 tool_result 合并到去重 map 中
// 如果 toolUseId 已存在，合并 content 数组（与 Python 版本 _merge_tool_result_into_dict 保持一致）
func mergeToolResultIntoMap(resultsByID map[string]*models.ToolResult, order *[]string, tr models.ToolResult) {
	if tr.ToolUseID == "" {
		return
	}

	if existing, ok := resultsByID[tr.ToolUseID]; ok {
		// 合并 content 数组，按 text 值去重
		existingTexts := make(map[string]bool)
		for _, c := range existing.Content {
			existingTexts[c.Text] = true
		}
		for _, c := range tr.Content {
			if c.Text != "" && !existingTexts[c.Text] {
				existing.Content = append(existing.Content, c)
				existingTexts[c.Text] = true
			}
		}
		// 如果任何结果有错误状态，保持错误
		if tr.Status == "error" {
			existing.Status = "error"
		}
		logger.Debug("[消息合并] 合并重复的 toolUseId: %s", tr.ToolUseID)
	} else {
		// 新的 toolUseId，添加到 map
		trCopy := tr
		resultsByID[tr.ToolUseID] = &trCopy
		*order = append(*order, tr.ToolUseID)
	}
}

// sanitizeJSONSchema 递归清理 JSON Schema 中 Amazon Q 不接受的字段
// 与 kiro-gateway 的 sanitize_json_schema 保持一致：
// - 移除所有层级的 additionalProperties（Amazon Q 不支持）
// - 移除空的 required 数组
// - 移除值为 null 的字段
// - 递归处理 properties、anyOf、oneOf 等嵌套结构
// unsupportedSchemaKeywords 列出 Amazon Q / CodeWhisperer 工具 schema 验证器不接受的 JSON Schema 关键字。
// 命中的键会在 sanitizeJSONSchema 中被剥离；命中 anyOf/oneOf 的字符串枚举联合会先尝试合并为单 enum。
var unsupportedSchemaKeywords = map[string]bool{
	"additionalProperties":  true,
	"$schema":               true,
	"$id":                   true,
	"$ref":                  true,
	"$defs":                 true,
	"definitions":           true,
	"anyOf":                 true,
	"oneOf":                 true,
	"allOf":                 true,
	"not":                   true,
	"propertyNames":         true,
	"patternProperties":     true,
	"dependencies":          true,
	"dependentRequired":     true,
	"dependentSchemas":      true,
	"if":                    true,
	"then":                  true,
	"else":                  true,
	"contains":              true,
	"minContains":           true,
	"maxContains":           true,
	"unevaluatedProperties": true,
	"unevaluatedItems":      true,
}

// tryCollapseUnionEnums 尝试把形如 anyOf/oneOf 的 string-枚举联合合并为单个 enum。
// 典型场景：TypeScript 风格的 union type（例如 "pending" | "in_progress" | "deleted"），
// 上游不接受 anyOf，但等价于一个合并的 enum。
// 仅当所有分支都是相同 type 的 scalar，且通过 enum 或 const 约束值时才合并，否则返回 nil（由上层剥离关键字）。
func tryCollapseUnionEnums(schema map[string]interface{}) map[string]interface{} {
	var branches []interface{}
	var unionKey string
	for _, k := range []string{"anyOf", "oneOf"} {
		if arr, ok := schema[k].([]interface{}); ok && len(arr) > 0 {
			branches = arr
			unionKey = k
			break
		}
	}
	if branches == nil {
		return nil
	}
	var merged []interface{}
	commonType := ""
	for _, b := range branches {
		m, ok := b.(map[string]interface{})
		if !ok {
			return nil
		}
		t, _ := m["type"].(string)
		if t == "" {
			return nil
		}
		if commonType == "" {
			commonType = t
		} else if commonType != t {
			return nil
		}
		if en, ok := m["enum"].([]interface{}); ok {
			merged = append(merged, en...)
		} else if c, ok := m["const"]; ok {
			merged = append(merged, c)
		} else {
			return nil
		}
	}
	if len(merged) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(schema))
	for k, v := range schema {
		if k == unionKey {
			continue
		}
		out[k] = v
	}
	out["type"] = commonType
	out["enum"] = merged
	return out
}

func sanitizeJSONSchema(schema map[string]interface{}) map[string]interface{} {
	if len(schema) == 0 {
		return schema
	}

	// 优先尝试把 anyOf/oneOf 的 string 枚举联合合并为单个 enum，保留语义
	if collapsed := tryCollapseUnionEnums(schema); collapsed != nil {
		schema = collapsed
	}

	result := make(map[string]interface{}, len(schema))
	for key, val := range schema {
		// 移除 null 值
		if val == nil {
			continue
		}
		// 跳过所有上游不支持的关键字（含 anyOf/oneOf/allOf/$ref/$defs/propertyNames 等）
		if unsupportedSchemaKeywords[key] {
			continue
		}
		// const 转 enum，保留约束语义
		if key == "const" {
			result["enum"] = []interface{}{val}
			continue
		}
		// 移除空的 required 数组
		if key == "required" {
			if arr, ok := val.([]interface{}); ok && len(arr) == 0 {
				continue
			}
		}

		// 递归处理 properties 内的每个属性 schema
		if key == "properties" {
			if propsMap, ok := val.(map[string]interface{}); ok {
				cleanedProps := make(map[string]interface{}, len(propsMap))
				for propName, propVal := range propsMap {
					if propSchema, ok := propVal.(map[string]interface{}); ok {
						cleanedProps[propName] = sanitizeJSONSchema(propSchema)
					} else {
						cleanedProps[propName] = propVal
					}
				}
				result[key] = cleanedProps
				continue
			}
		}

		// 递归处理嵌套对象（如 items 等）
		if nested, ok := val.(map[string]interface{}); ok {
			result[key] = sanitizeJSONSchema(nested)
			continue
		}

		// 递归处理数组元素（items 可能是 []schema 形式）
		if arr, ok := val.([]interface{}); ok {
			cleanedArr := make([]interface{}, 0, len(arr))
			for _, item := range arr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					cleanedArr = append(cleanedArr, sanitizeJSONSchema(itemMap))
				} else {
					cleanedArr = append(cleanedArr, item)
				}
			}
			result[key] = cleanedArr
			continue
		}

		result[key] = val
	}
	return result
}

// extractClaudeTextContent 从 Claude 内容中提取文本（包括 thinking 内容）
func extractClaudeTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, block := range c {
			if m, ok := block.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				if blockType == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				} else if blockType == "thinking" {
					// 将 thinking 内容包装成 XML 标签
					if thinking, ok := m["thinking"].(string); ok {
						parts = append(parts, wrapThinkingContent(thinking))
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// extractImagesFromContent 从 Claude 内容中提取图片并转换为 Amazon Q 格式
// 支持两种格式：
// 1. Anthropic 原生格式: {"type": "image", "source": {"type": "base64", "media_type": "...", "data": "..."}}
// 2. OpenAI 格式: {"type": "image_url", "image_url": {"url": "data:image/...;base64,..."}}
func extractImagesFromContent(content interface{}) []models.AmazonQImage {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	var images []models.AmazonQImage
	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		blockType, _ := m["type"].(string)

		switch blockType {
		case "image":
			// Anthropic 原生格式
			source, ok := m["source"].(map[string]interface{})
			if !ok {
				continue
			}

			// 只支持 base64 类型的图片
			if source["type"] != "base64" {
				continue
			}

			mediaType, _ := source["media_type"].(string)
			if mediaType == "" {
				mediaType = "image/png"
			}

			data, _ := source["data"].(string)
			if data == "" {
				continue
			}

			// 通过魔数校正被错误声明的 media_type —— 上游 Amazon Q 会按字节严格校验，
			// 声明 jpeg 但实际是 PNG/WebP 会被拒为 Improperly formed request。
			if detected := utils.DetectImageFormatFromBase64(data); detected != "" && detected != mediaType {
				logger.Warn("[消息转换] 图片 media_type 声明为 %s 但实际为 %s，已按实际格式转发", mediaType, detected)
				mediaType = detected
			}

			// 从 media_type 提取格式（如 image/png -> png）
			format := "png"
			if idx := strings.LastIndex(mediaType, "/"); idx != -1 {
				format = mediaType[idx+1:]
			}

			images = append(images, models.AmazonQImage{
				Format: format,
				Source: models.AmazonQImageSource{
					Bytes: data,
				},
			})

		case "image_url":
			// OpenAI 格式: {"type": "image_url", "image_url": {"url": "data:..."}}
			imageURL, ok := m["image_url"].(map[string]interface{})
			if !ok {
				continue
			}

			// 使用工具函数转换
			aqImage, err := utils.ConvertImageURLToAmazonQImage(imageURL)
			if err != nil {
				logger.Warn("[消息转换] 转换 image_url 失败: %v", err)
				continue
			}

			images = append(images, *aqImage)
		}
	}

	if len(images) == 0 {
		return nil
	}
	return images
}

// extractClaudeToolResults 从 Claude 内容中提取工具结果
func extractClaudeToolResults(content interface{}) []models.ToolResult {
	blocks, ok := content.([]interface{})
	if !ok {
		return nil
	}

	resultMap := make(map[string]*models.ToolResult)
	var order []string

	for _, block := range blocks {
		m, ok := block.(map[string]interface{})
		if !ok || m["type"] != "tool_result" {
			continue
		}

		toolUseID, _ := m["tool_use_id"].(string)
		rawContent := m["content"]

		// 检查 status 和 is_error 字段（与参考项目一致）
		status, _ := m["status"].(string)
		isError, _ := m["is_error"].(bool)
		if status == "" {
			// 如果 status 未设置，从 is_error 推断
			if isError {
				status = "error"
			} else {
				status = "success"
			}
		}

		var aqContent []models.ToolResultContent
		switch c := rawContent.(type) {
		case string:
			aqContent = []models.ToolResultContent{{Text: c}}
		case []interface{}:
			for _, item := range c {
				if im, ok := item.(map[string]interface{}); ok {
					if im["type"] == "text" {
						if text, ok := im["text"].(string); ok {
							aqContent = append(aqContent, models.ToolResultContent{Text: text})
						}
					} else if text, ok := im["text"].(string); ok {
						aqContent = append(aqContent, models.ToolResultContent{Text: text})
					}
				} else if s, ok := item.(string); ok {
					aqContent = append(aqContent, models.ToolResultContent{Text: s})
				}
			}
		}

		hasContent := false
		for _, c := range aqContent {
			if c.Text != "" {
				hasContent = true
				break
			}
		}
		if !hasContent {
			// 根据状态返回不同的默认消息（与参考项目一致）
			if status != "error" && !isError {
				aqContent = []models.ToolResultContent{{Text: "Command executed successfully"}}
			} else {
				aqContent = []models.ToolResultContent{{Text: "Tool use was cancelled by the user"}}
			}
		}

		if existing, ok := resultMap[toolUseID]; ok {
			existing.Content = append(existing.Content, aqContent...)
			// 如果当前是错误状态，更新状态
			if status == "error" {
				existing.Status = "error"
			}
		} else {
			resultMap[toolUseID] = &models.ToolResult{
				ToolUseID: toolUseID,
				Content:   aqContent,
				Status:    status,
			}
			order = append(order, toolUseID)
		}
	}

	var results []models.ToolResult
	for _, id := range order {
		results = append(results, *resultMap[id])
	}
	return results
}

// processClaudeHistoryWithMessageID 处理 Claude 历史消息，转换为 Amazon Q 格式
func processClaudeHistoryWithMessageID(messages []models.ClaudeMessage, thinkingEnabled bool, modelID string) []models.HistoryMessage {
	var history []models.HistoryMessage
	seenToolUseIDs := make(map[string]bool)
	var lastToolUseOrder []string
	var lastMsgType string // "user", "assistant"
	toolUseIdToName := make(map[string]string)

	for _, msg := range messages {
		if msg.Role == "user" {
			// 检测连续 user 消息，插入 "OK" 的 assistant
			if lastMsgType == "user" {
				history = append(history, models.HistoryMessage{
					AssistantResponseMessage: &models.AssistantResponseMessage{
						Content: "OK",
					},
				})
			}

			textContent := extractClaudeTextContent(msg.Content)
			toolResults := extractClaudeToolResults(msg.Content)
			images := extractImagesFromContent(msg.Content)

			// 根据上一个 assistant 消息中的 tool_use 顺序重新排序 tool_results
			if len(toolResults) > 0 && len(lastToolUseOrder) > 0 {
				toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
			}

			// 如果启用了 thinking 模式，添加 thinking 提示
			if thinkingEnabled && textContent != "" {
				textContent = appendThinkingHint(textContent)
			}

			userCtx := &models.UserInputMessageContext{
				EnvState: models.EnvState{
					OperatingSystem:         getOperatingSystem(),
					CurrentWorkingDirectory: "/",
				},
			}
			if len(toolResults) > 0 {
				userCtx.ToolResults = toolResults
			}

			// content 为空时兜底，避免上游拒绝空内容
			if textContent == "" && len(toolResults) > 0 {
				textContent = "Tool results provided."
			} else if textContent == "" {
				textContent = "Continue"
			}

			history = append(history, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content:                 textContent,
					UserInputMessageContext: *userCtx,
					Origin:                  "AI_EDITOR",
					ModelID:                 modelID,
					Images:                  images,
				},
			})
			lastMsgType = "user"

		} else if msg.Role == "assistant" {
			textContent := extractClaudeTextContent(msg.Content)

			// 跳过包含错误信息的助手消息
			if strings.HasPrefix(textContent, "❌ 请求失败:") {
				continue
			}

			// 检测连续 assistant 消息，插入 "Continue" 的 user
			if lastMsgType == "assistant" {
				history = append(history, models.HistoryMessage{
					UserInputMessage: &models.UserInputMessage{
						Content: "Continue",
						UserInputMessageContext: models.UserInputMessageContext{
							EnvState: models.EnvState{
								OperatingSystem:         getOperatingSystem(),
								CurrentWorkingDirectory: "/",
							},
						},
						Origin:  "AI_EDITOR",
						ModelID: modelID,
					},
				})
			}

			// assistant content 为空时兜底（仅有 tool_use 无文本的情况）
			if textContent == "" {
				textContent = "..."
			}

			entry := models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: textContent,
				},
			}

			// 追踪 tool_use 顺序
			lastToolUseOrder = nil
			if blocks, ok := msg.Content.([]interface{}); ok {
				var toolUses []models.ToolUse
				for _, block := range blocks {
					m, ok := block.(map[string]interface{})
					if !ok || m["type"] != "tool_use" {
						continue
					}
					tid, _ := m["id"].(string)
					if tid == "" {
						continue
					}
					if seenToolUseIDs[tid] {
						continue
					}
					seenToolUseIDs[tid] = true
					lastToolUseOrder = append(lastToolUseOrder, tid)

					name, _ := m["name"].(string)
					input, _ := m["input"].(map[string]interface{})
					// 与 tools[] 一致地规整：超长名截断到 64 字符以内，否则上游拒绝。
					name = sanitizeToolName(name)
					toolUseIdToName[tid] = name

					toolUses = append(toolUses, models.ToolUse{
						ToolUseID: tid,
						Name:      name,
						Input:     input,
					})
				}
				if len(toolUses) > 0 {
					entry.AssistantResponseMessage.ToolUses = toolUses
				}
			}

			history = append(history, entry)
			lastMsgType = "assistant"
		}
	}

	// 确保历史以 assistant 消息结尾
	if len(history) > 0 {
		lastItem := history[len(history)-1]
		if lastItem.UserInputMessage != nil {
			// 最后是 user 消息，补充一个 "OK" 的 assistant
			history = append(history, models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: "OK",
				},
			})
		}
	}

	return history
}

// processClaudeHistory 处理 Claude 历史消息，转换为 Amazon Q 格式（交替的 user/assistant 消息）
// 双模式检测：
// - 如果消息已正确交替（无连续的相同角色消息），跳过合并逻辑（快速路径）
// - 如果检测到连续的相同角色消息，应用合并逻辑
// 关键修复：追踪 assistant 消息中的 tool_use 顺序，并在下一个 user 消息中重新排序 tool_results 以匹配
// Deprecated: 建议使用 processClaudeHistoryWithMessageID
func processClaudeHistory(messages []models.ClaudeMessage, thinkingEnabled bool) []models.HistoryMessage {
	var rawHistory []models.HistoryMessage
	seenToolUseIDs := make(map[string]bool)
	var lastToolUseOrder []string // 追踪上一个 assistant 消息中的 tool_use 顺序

	// 第一遍：转换单个消息
	for _, msg := range messages {
		if msg.Role == "user" {
			textContent := extractClaudeTextContent(msg.Content)
			toolResults := extractClaudeToolResults(msg.Content)
			images := extractImagesFromContent(msg.Content)

			// 根据上一个 assistant 消息中的 tool_use 顺序重新排序 tool_results
			if len(toolResults) > 0 && len(lastToolUseOrder) > 0 {
				toolResults = reorderToolResultsByToolUses(toolResults, lastToolUseOrder)
				logger.Debug("[历史处理] 重新排序 %d 个 tool_results 以匹配 tool_use 顺序", len(toolResults))
			}

			// 如果启用了 thinking 模式，添加 thinking 提示
			if thinkingEnabled && textContent != "" {
				textContent = appendThinkingHint(textContent)
			}

			userCtx := &models.UserInputMessageContext{
				EnvState: models.EnvState{
					OperatingSystem:         getOperatingSystem(),
					CurrentWorkingDirectory: "/",
				},
			}
			if len(toolResults) > 0 {
				userCtx.ToolResults = toolResults
			}

			// 当 content 为空但有 toolResults 时，添加默认提示
			if textContent == "" && len(toolResults) > 0 {
				textContent = "<system-reminder>user said something to assistant, but report the tools result. follow tool results.</system-reminder>"
			}

			uMsg := &models.UserInputMessage{
				Content:                 textContent,
				UserInputMessageContext: *userCtx,
				Origin:                  "KIRO_CLI",
				Images:                  images,
			}

			rawHistory = append(rawHistory, models.HistoryMessage{UserInputMessage: uMsg})

		} else if msg.Role == "assistant" {
			textContent := extractClaudeTextContent(msg.Content)

			// 跳过包含错误信息的助手消息，避免发送给 Amazon Q 导致验证失败
			// 注意：只检查特定的错误前缀，不要使用宽泛的 "error": 匹配
			// 因为正常消息中可能包含 JSON 示例或代码讨论中的 "error": 字符串
			if strings.HasPrefix(textContent, "❌ 请求失败:") {
				continue
			}

			// assistant content 为空时兜底
			if textContent == "" {
				textContent = "..."
			}

			entry := models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: textContent,
				},
			}

			// 追踪 tool_use 顺序，用于在下一个 user 消息中重新排序 tool_results
			lastToolUseOrder = nil
			if blocks, ok := msg.Content.([]interface{}); ok {
				var toolUses []models.ToolUse
				for _, block := range blocks {
					m, ok := block.(map[string]interface{})
					if !ok || m["type"] != "tool_use" {
						continue
					}
					tid, _ := m["id"].(string)
					if tid == "" {
						continue
					}
					if seenToolUseIDs[tid] {
						logger.Debug("[历史处理] 跳过重复的 tool_use: %s", tid)
						continue
					}
					seenToolUseIDs[tid] = true
					lastToolUseOrder = append(lastToolUseOrder, tid) // 追踪顺序

					name, _ := m["name"].(string)
					input, _ := m["input"].(map[string]interface{})
					toolUses = append(toolUses, models.ToolUse{
						ToolUseID: tid,
						Name:      name,
						Input:     input,
					})
				}
				if len(toolUses) > 0 {
					entry.AssistantResponseMessage.ToolUses = toolUses
				}
			}

			rawHistory = append(rawHistory, entry)
		}
	}

	// 双模式检测：检查消息是否已正确交替
	hasConsecutiveSameRole := false
	var prevRole string
	for _, item := range rawHistory {
		var currentRole string
		if item.UserInputMessage != nil {
			currentRole = "user"
		} else {
			currentRole = "assistant"
		}
		if prevRole == currentRole {
			hasConsecutiveSameRole = true
			break
		}
		prevRole = currentRole
	}

	// 如果消息已正确交替，跳过合并逻辑（快速路径）
	if !hasConsecutiveSameRole {
		logger.Debug("[历史处理] 消息已正确交替，跳过合并，返回 %d 条历史", len(rawHistory))
		return rawHistory
	}

	logger.Debug("[历史处理] 检测到连续相同角色消息，执行合并逻辑")

	// 第二遍：合并连续的用户消息（仅在需要时执行）
	// 重要：包含 toolResults 的用户消息应该保持独立，不与普通文本消息合并
	// 这样可以确保 AI 能正确看到工具执行结果，避免无限循环
	var history []models.HistoryMessage
	var pendingUserMsgs []*models.UserInputMessage

	for _, item := range rawHistory {
		if item.UserInputMessage != nil {
			msg := item.UserInputMessage
			hasToolResults := len(msg.UserInputMessageContext.ToolResults) > 0

			if hasToolResults {
				// 包含 toolResults 的消息：先处理待合并的普通消息，然后独立添加此消息
				if len(pendingUserMsgs) > 0 {
					merged := mergeClaudeUserMessages(pendingUserMsgs)
					history = append(history, models.HistoryMessage{UserInputMessage: merged})
					pendingUserMsgs = nil
				}
				// toolResults 消息保持独立
				history = append(history, models.HistoryMessage{UserInputMessage: msg})
			} else {
				// 普通文本消息：加入待合并队列
				pendingUserMsgs = append(pendingUserMsgs, msg)
			}
		} else if item.AssistantResponseMessage != nil {
			if len(pendingUserMsgs) > 0 {
				merged := mergeClaudeUserMessages(pendingUserMsgs)
				history = append(history, models.HistoryMessage{UserInputMessage: merged})
				pendingUserMsgs = nil
			}
			history = append(history, item)
		}
	}

	if len(pendingUserMsgs) > 0 {
		merged := mergeClaudeUserMessages(pendingUserMsgs)
		history = append(history, models.HistoryMessage{UserInputMessage: merged})
	}

	// 后处理：确保历史消息严格交替（user-assistant-user-assistant）
	history = ensureAlternatingMessages(history)

	return history
}

// ensureAlternatingMessages 确保历史消息严格交替（user-assistant-user-assistant）
// 处理三种情况：
// 1. 第一条消息是 assistant，在前面插入空 user
// 2. 连续 user 消息，在中间插入空 assistant
// 3. 连续 assistant 消息，在中间插入空 user
func ensureAlternatingMessages(history []models.HistoryMessage) []models.HistoryMessage {
	if len(history) == 0 {
		return history
	}

	var result []models.HistoryMessage
	var prevRole string

	for _, item := range history {
		var currentRole string
		if item.UserInputMessage != nil {
			currentRole = "user"
		} else {
			currentRole = "assistant"
		}

		// 第一条消息必须是 user，如果是 assistant 则在前面插入空 user
		if len(result) == 0 && currentRole == "assistant" {
			logger.Debug("[历史处理] 第一条是 assistant，插入空 user 占位消息")
			result = append(result, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content: "...",
				},
			})
			prevRole = "user"
		}

		// 连续 user 消息，插入空 assistant
		if prevRole == "user" && currentRole == "user" {
			logger.Debug("[历史处理] 检测到连续 user 消息，插入空 assistant 占位消息")
			result = append(result, models.HistoryMessage{
				AssistantResponseMessage: &models.AssistantResponseMessage{
					Content: "...",
				},
			})
		}

		// 连续 assistant 消息，插入空 user
		if prevRole == "assistant" && currentRole == "assistant" {
			logger.Debug("[历史处理] 检测到连续 assistant 消息，插入空 user 占位消息")
			result = append(result, models.HistoryMessage{
				UserInputMessage: &models.UserInputMessage{
					Content: "...",
				},
			})
		}

		result = append(result, item)
		prevRole = currentRole
	}

	return result
}

// mergeClaudeUserMessages 合并用户消息（只保留最后 2 条消息的图片）
// 重要：此函数正确合并所有消息的 toolResults，以防止丢失工具执行历史导致无限循环
// 关键修复：按 toolUseId 去重 toolResults，防止重复的 tool_result 导致模型重复响应
// 合并时移除重复的 thinking hints，确保只在末尾出现一次
func mergeClaudeUserMessages(msgs []*models.UserInputMessage) *models.UserInputMessage {
	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) == 1 {
		return msgs[0]
	}

	var allContents []string
	var baseCtx *models.UserInputMessageContext
	var baseOrigin string
	var allImages [][]models.AmazonQImage
	// 使用 map 按 toolUseId 去重 toolResults（与 Python 版本保持一致）
	toolResultsByID := make(map[string]*models.ToolResult)
	var toolResultOrder []string // 保持顺序

	// 检查是否有消息包含 thinking hint
	hadThinkingHint := false

	for i, m := range msgs {
		// 初始化基础上下文（从第一个消息）
		if i == 0 {
			// 复制第一个消息的上下文
			ctxCopy := m.UserInputMessageContext
			baseCtx = &ctxCopy
			baseOrigin = m.Origin

			// 从基础上下文中提取 toolResults 并清空（单独合并）
			if len(baseCtx.ToolResults) > 0 {
				for _, tr := range baseCtx.ToolResults {
					mergeToolResultIntoMap(toolResultsByID, &toolResultOrder, tr)
				}
				baseCtx.ToolResults = nil
			}
		} else {
			// 从后续消息收集 toolResults（按 toolUseId 去重合并）
			if len(m.UserInputMessageContext.ToolResults) > 0 {
				for _, tr := range m.UserInputMessageContext.ToolResults {
					mergeToolResultIntoMap(toolResultsByID, &toolResultOrder, tr)
				}
			}
		}

		// 处理内容：移除 thinking hint 以避免重复
		content := m.Content
		if content != "" {
			if strings.Contains(content, ThinkingHint) {
				hadThinkingHint = true
				content = strings.ReplaceAll(content, ThinkingHint, "")
				content = strings.TrimSpace(content)
			}
			if content != "" {
				allContents = append(allContents, content)
			}
		}

		// 收集图片
		if len(m.Images) > 0 {
			allImages = append(allImages, m.Images)
		}
	}

	// 合并内容
	mergedContent := strings.Join(allContents, "\n\n")

	// 如果原始消息有 thinking hint，在合并内容末尾添加一次
	if hadThinkingHint && mergedContent != "" {
		mergedContent = appendThinkingHint(mergedContent)
	}

	result := &models.UserInputMessage{
		Content:                 mergedContent,
		UserInputMessageContext: *baseCtx,
		Origin:                  baseOrigin,
	}

	// 将去重后的 toolResults 按顺序添加到结果
	if len(toolResultsByID) > 0 {
		var mergedToolResults []models.ToolResult
		for _, id := range toolResultOrder {
			if tr, ok := toolResultsByID[id]; ok {
				mergedToolResults = append(mergedToolResults, *tr)
			}
		}
		result.UserInputMessageContext.ToolResults = mergedToolResults
	}

	// 只保留最后 2 条消息的图片（与 Python 参考项目一致）
	if len(allImages) > 0 {
		var keptImages []models.AmazonQImage
		start := len(allImages) - 2
		if start < 0 {
			start = 0
		}
		for _, imgList := range allImages[start:] {
			keptImages = append(keptImages, imgList...)
		}
		if len(keptImages) > 0 {
			result.Images = keptImages
		}
	}

	return result
}
