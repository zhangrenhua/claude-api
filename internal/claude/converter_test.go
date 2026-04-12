package claude

import (
	"claude-api/internal/models"
	"encoding/json"
	"strings"
	"testing"
)

// TestConvertClaudeToAmazonQ_BasicMessage 测试基本消息转换
func TestConvertClaudeToAmazonQ_BasicMessage(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 conversationId 不为空
	if result.ConversationState.ConversationID == "" {
		t.Error("conversationId 不应为空")
	}

	// 验证 content 直接使用原始文本（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Hello, how are you?" {
		t.Errorf("content 应为原始文本，got: %s", content)
	}

	// 验证 origin 为 "AI_EDITOR"（与参考项目一致）
	origin := result.ConversationState.CurrentMessage.UserInputMessage.Origin
	if origin != "AI_EDITOR" {
		t.Errorf("origin 应为 'AI_EDITOR', got: %s", origin)
	}

	// 验证 chatTriggerType 为 "MANUAL"
	if result.ConversationState.ChatTriggerType != "MANUAL" {
		t.Errorf("chatTriggerType 应为 'MANUAL', got: %s", result.ConversationState.ChatTriggerType)
	}
}

// TestConvertClaudeToAmazonQ_WithHistory 测试带历史消息的转换
func TestConvertClaudeToAmazonQ_WithHistory(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What is 2+2?"},
			{Role: "assistant", Content: "2+2 equals 4."},
			{Role: "user", Content: "What about 3+3?"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史消息存在
	if len(result.ConversationState.History) == 0 {
		t.Error("历史消息不应为空")
	}

	// 验证历史消息是 tagged union 格式（无顶层 messageId）
	for i, msg := range result.ConversationState.History {
		if msg.UserInputMessage == nil && msg.AssistantResponseMessage == nil {
			t.Errorf("历史消息 %d 应有 userInputMessage 或 assistantResponseMessage", i)
		}
	}

	// 验证当前消息
	if result.ConversationState.CurrentMessage.UserInputMessage.Content != "What about 3+3?" {
		t.Errorf("当前消息内容不正确")
	}
}

// TestConvertClaudeToAmazonQ_WithSystemPrompt 测试带 system prompt 的转换
func TestConvertClaudeToAmazonQ_WithSystemPrompt(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "You are a helpful assistant.",
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 system prompt 转换为 user-assistant 消息对（与参考项目一致）
	if len(result.ConversationState.History) < 2 {
		t.Error("system prompt 应转换为至少 2 条历史消息")
	}

	// 第一条应该是 user（包含 system prompt）
	firstMsg := result.ConversationState.History[0]
	if firstMsg.UserInputMessage == nil {
		t.Error("第一条历史消息应为 userInputMessage")
	}
	if !strings.Contains(firstMsg.UserInputMessage.Content, "helpful assistant") {
		t.Error("第一条历史消息应包含 system prompt 内容")
	}

	// 第二条应该是 assistant（回复 "OK"）
	if len(result.ConversationState.History) >= 2 {
		secondMsg := result.ConversationState.History[1]
		if secondMsg.AssistantResponseMessage == nil {
			t.Error("第二条历史消息应为 assistantResponseMessage")
		}
		if secondMsg.AssistantResponseMessage.Content != "OK" {
			t.Errorf("第二条历史消息内容应为 'OK', got: %s", secondMsg.AssistantResponseMessage.Content)
		}
	}
}

// TestConvertClaudeToAmazonQ_WithTools 测试带工具的转换
func TestConvertClaudeToAmazonQ_WithTools(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []models.ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather information",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"location"},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证工具存在
	tools := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools
	if len(tools) != 1 {
		t.Errorf("应有 1 个工具, got: %d", len(tools))
	}

	// 验证工具名称
	if tools[0].ToolSpecification.Name != "get_weather" {
		t.Errorf("工具名称应为 'get_weather', got: %s", tools[0].ToolSpecification.Name)
	}
}

// TestConvertClaudeToAmazonQ_WithToolResults 测试带工具结果的转换
func TestConvertClaudeToAmazonQ_WithToolResults(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Tools: []models.ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Check the weather"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{
						"type": "tool_use",
						"id":   "tool_123",
						"name": "get_weather",
						"input": map[string]interface{}{
							"city": "Beijing",
						},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Temperature is 25°C",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 toolResults 存在
	toolResults := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults
	if len(toolResults) != 1 {
		t.Fatalf("应有 1 个 toolResult, got: %d", len(toolResults))
	}

	// 验证 tool_use_id
	if toolResults[0].ToolUseID != "tool_123" {
		t.Errorf("toolUseId 应为 'tool_123', got: %s", toolResults[0].ToolUseID)
	}

	// 验证 content 为默认值（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Tool results provided." {
		t.Errorf("content 应为 'Tool results provided.', got: %s", content)
	}
}

// TestConvertClaudeToAmazonQ_ToolResultWithUserText 测试 toolResult 同时带用户文本
func TestConvertClaudeToAmazonQ_ToolResultWithUserText(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Sunny, 25°C",
					},
					map[string]interface{}{
						"type": "text",
						"text": "Please summarize this weather info.",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 content 为用户文本（与参考项目一致）
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Please summarize this weather info." {
		t.Errorf("content 应为用户文本, got: %s", content)
	}
}

// TestDetermineChatTriggerType 测试 chatTriggerType 始终返回 MANUAL
func TestDetermineChatTriggerType(t *testing.T) {
	tests := []struct {
		name             string
		req              *models.ClaudeRequest
		filteredToolCount int
		expected         string
	}{
		{
			name: "无工具 - MANUAL",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
			},
			filteredToolCount: 0,
			expected:          "MANUAL",
		},
		{
			name: "有工具但无 tool_choice - MANUAL",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:    []models.ClaudeTool{{Name: "test_tool"}},
			},
			filteredToolCount: 1,
			expected:          "MANUAL",
		},
		{
			name: "有工具且 tool_choice=any - 仍然 MANUAL",
			req: &models.ClaudeRequest{
				Messages:   []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:      []models.ClaudeTool{{Name: "test_tool"}},
				ToolChoice: map[string]interface{}{"type": "any"},
			},
			filteredToolCount: 1,
			expected:          "MANUAL",
		},
		{
			name: "有工具且 tool_choice=tool - 仍然 MANUAL",
			req: &models.ClaudeRequest{
				Messages:   []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:      []models.ClaudeTool{{Name: "test_tool"}},
				ToolChoice: map[string]interface{}{"type": "tool", "name": "test_tool"},
			},
			filteredToolCount: 1,
			expected:          "MANUAL",
		},
		{
			name: "原始有工具但全部被过滤 - MANUAL",
			req: &models.ClaudeRequest{
				Messages:   []models.ClaudeMessage{{Role: "user", Content: "Hello"}},
				Tools:      []models.ClaudeTool{{Name: "web_search"}},
				ToolChoice: map[string]interface{}{"type": "tool", "name": "web_search"},
			},
			filteredToolCount: 0,
			expected:          "MANUAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineChatTriggerType(tt.req, tt.filteredToolCount)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestConvertClaudeToAmazonQ_ConsecutiveUserMessages 测试连续 user 消息处理
func TestConvertClaudeToAmazonQ_ConsecutiveUserMessages(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Question 1"},
			{Role: "user", Content: "Question 2"},
			{Role: "user", Content: "Question 3"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史消息交替（连续 user 之间应插入 "OK" 的 assistant）
	history := result.ConversationState.History
	var prevType string
	for i, msg := range history {
		var currentType string
		if msg.UserInputMessage != nil {
			currentType = "user"
		} else {
			currentType = "assistant"
		}
		if i > 0 && prevType == currentType {
			t.Errorf("历史消息 %d 和 %d 应交替, 都是 %s", i-1, i, currentType)
		}
		prevType = currentType
	}
}

// TestConvertClaudeToAmazonQ_HistoryEndsWithAssistant 测试历史以 assistant 结尾
func TestConvertClaudeToAmazonQ_HistoryEndsWithAssistant(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Question 1"},
			{Role: "user", Content: "Question 2"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史以 assistant 结尾
	history := result.ConversationState.History
	if len(history) > 0 {
		lastMsg := history[len(history)-1]
		if lastMsg.AssistantResponseMessage == nil {
			t.Error("历史应以 assistant 消息结尾")
		}
	}
}

// TestConvertClaudeToAmazonQ_OutputFormat 测试输出 JSON 格式
func TestConvertClaudeToAmazonQ_OutputFormat(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证可以正确序列化为 JSON
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	t.Logf("输出 JSON:\n%s", string(jsonBytes))

	// 验证结构完整性
	if result.ConversationState.ConversationID == "" {
		t.Error("conversationId 不应为空")
	}
	if result.ConversationState.ChatTriggerType == "" {
		t.Error("chatTriggerType 不应为空")
	}
}

// TestConvertClaudeToAmazonQ_WithToolUseHistory 测试带 tool_use 的历史消息（有工具定义）
func TestConvertClaudeToAmazonQ_WithToolUseHistory(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Tools: []models.ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "What's the weather?"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Let me check..."},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "tool_123",
						"name":  "get_weather",
						"input": map[string]interface{}{"city": "Beijing"},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_123",
						"content":     "Sunny, 25°C",
					},
				},
			},
			{Role: "user", Content: "Thanks!"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史中有 assistant 消息带 toolUses
	foundToolUse := false
	for _, msg := range result.ConversationState.History {
		if msg.AssistantResponseMessage != nil && len(msg.AssistantResponseMessage.ToolUses) > 0 {
			foundToolUse = true
			// 验证 toolUse 格式
			tu := msg.AssistantResponseMessage.ToolUses[0]
			if tu.ToolUseID != "tool_123" {
				t.Errorf("toolUseId 应为 'tool_123', got: %s", tu.ToolUseID)
			}
			if tu.Name != "get_weather" {
				t.Errorf("name 应为 'get_weather', got: %s", tu.Name)
			}
			break
		}
	}
	if !foundToolUse {
		t.Error("历史中应有包含 toolUses 的 assistant 消息")
	}

	// 验证历史中有 user 消息带 toolResults
	foundToolResult := false
	for _, msg := range result.ConversationState.History {
		if msg.UserInputMessage != nil && len(msg.UserInputMessage.UserInputMessageContext.ToolResults) > 0 {
			foundToolResult = true
			tr := msg.UserInputMessage.UserInputMessageContext.ToolResults[0]
			if tr.ToolUseID != "tool_123" {
				t.Errorf("toolUseId 应为 'tool_123', got: %s", tr.ToolUseID)
			}
			break
		}
	}
	if !foundToolResult {
		t.Error("历史中应有包含 toolResults 的 user 消息")
	}
}

// TestConvertClaudeToAmazonQ_FlattenToolRefsWhenNoTools 测试无工具定义时平坦化工具引用
// 场景：Zed 编辑器发送标题生成请求，不携带 tools 字段，但历史中包含工具调用
func TestConvertClaudeToAmazonQ_FlattenToolRefsWhenNoTools(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		// 无 Tools 字段
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Check the weather"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Let me check..."},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "tool_abc",
						"name":  "get_weather",
						"input": map[string]interface{}{"city": "Beijing"},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_abc",
						"content":     "Sunny, 25°C",
					},
					map[string]interface{}{
						"type": "text",
						"text": "Generate a title",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证历史 assistant 消息中 toolUses 已被移除并转换为文本
	for _, msg := range result.ConversationState.History {
		if msg.AssistantResponseMessage != nil {
			if len(msg.AssistantResponseMessage.ToolUses) > 0 {
				t.Error("无工具定义时，历史 assistant 消息不应包含 toolUses")
			}
			// 工具名称应被转换为文本
			if !strings.Contains(msg.AssistantResponseMessage.Content, "get_weather") {
				t.Errorf("assistant 文本应包含工具名称, got: %s", msg.AssistantResponseMessage.Content)
			}
		}
	}

	// 验证历史 user 消息中 toolResults 已被移除
	for _, msg := range result.ConversationState.History {
		if msg.UserInputMessage != nil && len(msg.UserInputMessage.UserInputMessageContext.ToolResults) > 0 {
			t.Error("无工具定义时，历史 user 消息不应包含 toolResults")
		}
	}

	// 验证当前消息中 toolResults 已被移除
	currentToolResults := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults
	if len(currentToolResults) > 0 {
		t.Error("无工具定义时，当前消息不应包含 toolResults")
	}

	// 验证当前消息的文本内容保留
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "Generate a title" {
		t.Errorf("当前消息内容应为 'Generate a title', got: %s", content)
	}
}

// TestConvertClaudeToAmazonQ_FixDanglingToolUse 测试悬空 tool_use 自动注入合成 toolResult
// 场景：assistant 发起 tool_use，但用户跳过工具执行直接发送文本消息
func TestConvertClaudeToAmazonQ_FixDanglingToolUse(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 1024,
		Tools: []models.ClaudeTool{
			{
				Name:        "run_bash",
				Description: "Run a bash command",
				InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}}},
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "帮我看个文件"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "好的，我来看看。"},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "tool_dangling_1",
						"name":  "run_bash",
						"input": map[string]interface{}{"command": "cat /tmp/config.json"},
					},
				},
			},
			// 用户没有返回 tool_result，直接发了文本
			{Role: "user", Content: "算了不用看了，出了点问题"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证 currentMessage 被注入了合成 toolResult
	toolResults := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults
	if len(toolResults) != 1 {
		t.Fatalf("应注入 1 个合成 toolResult, got: %d", len(toolResults))
	}
	if toolResults[0].ToolUseID != "tool_dangling_1" {
		t.Errorf("toolUseId 应为 'tool_dangling_1', got: %s", toolResults[0].ToolUseID)
	}
	if toolResults[0].Status != "error" {
		t.Errorf("合成 toolResult 的 status 应为 'error', got: %s", toolResults[0].Status)
	}

	// 验证用户文本内容保留
	content := result.ConversationState.CurrentMessage.UserInputMessage.Content
	if content != "算了不用看了，出了点问题" {
		t.Errorf("用户文本内容应保留, got: %s", content)
	}
}

// TestConvertClaudeToAmazonQ_NoDanglingWhenToolResultExists 测试有 toolResult 时不误注入
func TestConvertClaudeToAmazonQ_NoDanglingWhenToolResultExists(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 1024,
		Tools: []models.ClaudeTool{
			{
				Name:        "run_bash",
				Description: "Run a bash command",
				InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}}},
			},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "看个文件"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "好的"},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "tool_normal_1",
						"name":  "run_bash",
						"input": map[string]interface{}{"command": "cat /tmp/test"},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_normal_1",
						"content":     "file content here",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 应只有原始的 1 个 toolResult，不应额外注入
	toolResults := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults
	if len(toolResults) != 1 {
		t.Fatalf("应有 1 个 toolResult (非注入), got: %d", len(toolResults))
	}
	if toolResults[0].Status == "error" {
		t.Error("原始 toolResult 不应被标记为 error")
	}
}

// TestConvertClaudeToAmazonQ_HistoryTaggedUnionFormat 测试历史消息为 tagged union 格式
func TestConvertClaudeToAmazonQ_HistoryTaggedUnionFormat(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System: []interface{}{
			map[string]interface{}{"type": "text", "text": "You are helpful."},
		},
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi!"},
			{Role: "user", Content: "Bye"},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// 验证所有历史消息是 tagged union 格式：只有 userInputMessage 或 assistantResponseMessage
	for i, msg := range result.ConversationState.History {
		hasUser := msg.UserInputMessage != nil
		hasAssistant := msg.AssistantResponseMessage != nil
		if !hasUser && !hasAssistant {
			t.Errorf("历史消息 %d 应有 userInputMessage 或 assistantResponseMessage", i)
		}
		if hasUser && hasAssistant {
			t.Errorf("历史消息 %d 不应同时有 userInputMessage 和 assistantResponseMessage", i)
		}
	}

	// 验证 JSON 输出中无顶层 messageId
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	// 检查 history 中不包含 "messageId" 作为顶层字段
	var raw map[string]interface{}
	json.Unmarshal(jsonBytes, &raw)
	cs := raw["conversationState"].(map[string]interface{})
	history := cs["history"].([]interface{})
	for i, entry := range history {
		entryMap := entry.(map[string]interface{})
		if _, hasMessageID := entryMap["messageId"]; hasMessageID {
			t.Errorf("历史消息 %d 不应有顶层 messageId 字段", i)
		}
	}
}

// TestConvertClaudeToAmazonQ_NoContentWrapping 测试 content 不被包装
func TestConvertClaudeToAmazonQ_NoContentWrapping(t *testing.T) {
	testContent := "This is a test message without any special formatting."
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: testContent},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	content := result.ConversationState.CurrentMessage.UserInputMessage.Content

	// 验证 content 不包含旧的包装格式
	unwantedPatterns := []string{
		"--- CONTEXT ENTRY BEGIN ---",
		"--- CONTEXT ENTRY END ---",
		"--- USER MESSAGE BEGIN ---",
		"--- USER MESSAGE END ---",
		"--- SYSTEM PROMPT BEGIN ---",
		"--- SYSTEM PROMPT END ---",
		"<system-reminder>",
	}

	for _, pattern := range unwantedPatterns {
		if strings.Contains(content, pattern) {
			t.Errorf("content 不应包含 '%s', got: %s", pattern, content)
		}
	}

	// 验证 content 为原始文本
	if content != testContent {
		t.Errorf("content 应为原始文本 '%s', got: %s", testContent, content)
	}
}

// TestConvertClaudeToAmazonQ_MCPTools 模拟 MCP 工具（GitHub/Chrome DevTools/SSH）的请求格式
// 验证 converter 对 MCP 特有特征的支持：
//   - 双下划线工具名 (mcp__server__tool)
//   - 复杂嵌套 schema（anyOf、$ref 替代物、深层 properties）
//   - 大量工具定义并存
//   - tool_use/tool_result 的完整生命周期
func TestConvertClaudeToAmazonQ_MCPTools(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 8192,
		System:    "You are a helpful assistant with access to MCP tools.",
		Tools: []models.ClaudeTool{
			// ---- GitHub MCP ----
			{
				Name:        "mcp__github__search_repositories",
				Description: "Search for GitHub repositories",
				InputSchema: map[string]interface{}{
					"$schema": "http://json-schema.org/draft-07/schema#",
					"type":    "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query",
						},
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "Page number",
						},
						"per_page": map[string]interface{}{
							"type":        "integer",
							"description": "Results per page",
						},
					},
					"required":             []interface{}{"query"},
					"additionalProperties": false,
				},
			},
			{
				Name:        "mcp__github__create_issue",
				Description: "Create a new issue in a GitHub repository",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"owner": map[string]interface{}{"type": "string"},
						"repo":  map[string]interface{}{"type": "string"},
						"title": map[string]interface{}{"type": "string"},
						"body":  map[string]interface{}{"type": "string"},
						"labels": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"assignees": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required":             []interface{}{"owner", "repo", "title"},
					"additionalProperties": false,
				},
			},
			{
				Name:        "mcp__github__get_file_contents",
				Description: "Get the contents of a file or directory from a GitHub repository",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"owner":  map[string]interface{}{"type": "string"},
						"repo":   map[string]interface{}{"type": "string"},
						"path":   map[string]interface{}{"type": "string"},
						"branch": map[string]interface{}{"type": "string", "description": "Branch name"},
					},
					"required":             []interface{}{"owner", "repo", "path"},
					"additionalProperties": false,
				},
			},
			// ---- Chrome DevTools MCP ----
			{
				Name:        "mcp__chrome__navigate",
				Description: "Navigate to a URL in the browser",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{"type": "string", "format": "uri"},
					},
					"required":             []interface{}{"url"},
					"additionalProperties": false,
				},
			},
			{
				Name:        "mcp__chrome__screenshot",
				Description: "Take a screenshot of the current page",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"selector": map[string]interface{}{
							"type":        "string",
							"description": "CSS selector to screenshot. If not provided, screenshots the full page.",
						},
						"width":  map[string]interface{}{"type": "integer"},
						"height": map[string]interface{}{"type": "integer"},
					},
					"additionalProperties": false,
					"required":             []interface{}{},
				},
			},
			{
				Name:        "mcp__chrome__evaluate",
				Description: "Execute JavaScript in the browser console",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{"type": "string"},
					},
					"required":             []interface{}{"expression"},
					"additionalProperties": false,
				},
			},
			// ---- SSH MCP ----
			{
				Name:        "mcp__ssh__execute_command",
				Description: "Execute a command on a remote server via SSH",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"host":    map[string]interface{}{"type": "string"},
						"command": map[string]interface{}{"type": "string"},
						"timeout": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
					},
					"required":             []interface{}{"host", "command"},
					"additionalProperties": false,
				},
			},
			// ---- Anthropic 内置工具（应被过滤） ----
			{
				Name:        "computer",
				Description: "Anthropic built-in computer use",
				InputSchema: nil,
			},
			{
				Name:        "web_search",
				Description: "Anthropic built-in web search",
				InputSchema: nil,
			},
		},
		Messages: []models.ClaudeMessage{
			// 1. 用户请求
			{Role: "user", Content: "帮我搜一下 GitHub 上的 claude-api 项目，然后用 SSH 看一下服务器状态"},
			// 2. assistant 发起两个并行 MCP tool_use
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "好的，我同时帮你搜索 GitHub 和检查服务器。"},
					map[string]interface{}{
						"type": "tool_use",
						"id":   "mcp_call_001",
						"name": "mcp__github__search_repositories",
						"input": map[string]interface{}{
							"query":    "claude-api language:go",
							"per_page": float64(5),
						},
					},
					map[string]interface{}{
						"type": "tool_use",
						"id":   "mcp_call_002",
						"name": "mcp__ssh__execute_command",
						"input": map[string]interface{}{
							"host":    "35.236.134.32",
							"command": "uptime && df -h",
							"timeout": float64(10),
						},
					},
				},
			},
			// 3. 并行工具返回结果
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "mcp_call_001",
						"content": []interface{}{
							map[string]interface{}{
								"type": "text",
								"text": "{\"total_count\":3,\"items\":[{\"full_name\":\"user/claude-api\",\"description\":\"Claude API proxy\",\"stargazers_count\":120}]}",
							},
						},
					},
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "mcp_call_002",
						"content":     "14:23:01 up 42 days, load average: 0.15, 0.10, 0.08\n/dev/sda1  50G  23G  25G  48% /",
					},
				},
			},
			// 4. assistant 分析结果并调用 Chrome
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "找到了项目，让我打开看看。"},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "mcp_call_003",
						"name":  "mcp__chrome__navigate",
						"input": map[string]interface{}{"url": "https://github.com/user/claude-api"},
					},
				},
			},
			// 5. Chrome 返回
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "mcp_call_003",
						"content":     "Navigated to https://github.com/user/claude-api",
					},
				},
			},
			// 6. assistant 再截图
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "mcp_call_004",
						"name":  "mcp__chrome__screenshot",
						"input": map[string]interface{}{},
					},
				},
			},
			// 7. 截图返回（含 image，这里简化为 text）
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "mcp_call_004",
						"content":     "[Screenshot captured: 1920x1080, page shows README with installation instructions]",
					},
					map[string]interface{}{
						"type": "text",
						"text": "看起来不错，帮我在这个项目上创建一个 issue",
					},
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "", false)
	if err != nil {
		t.Fatalf("MCP 工具请求转换失败: %v", err)
	}

	// ===== 验证工具定义 =====
	tools := result.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools
	// 应有 7 个 MCP 工具（2 个内置工具 computer/web_search 应被过滤）
	if len(tools) != 7 {
		t.Errorf("应有 7 个 MCP 工具（过滤 2 个内置），got: %d", len(tools))
		for i, tool := range tools {
			t.Logf("  tool[%d]: %s", i, tool.ToolSpecification.Name)
		}
	}

	// 验证工具名保留双下划线
	foundGitHub := false
	foundChrome := false
	foundSSH := false
	for _, tool := range tools {
		name := tool.ToolSpecification.Name
		switch {
		case strings.HasPrefix(name, "mcp__github__"):
			foundGitHub = true
		case strings.HasPrefix(name, "mcp__chrome__"):
			foundChrome = true
		case strings.HasPrefix(name, "mcp__ssh__"):
			foundSSH = true
		}
	}
	if !foundGitHub {
		t.Error("未找到 mcp__github__ 前缀的工具")
	}
	if !foundChrome {
		t.Error("未找到 mcp__chrome__ 前缀的工具")
	}
	if !foundSSH {
		t.Error("未找到 mcp__ssh__ 前缀的工具")
	}

	// ===== 验证 schema 清洗 =====
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	jsonStr := string(jsonBytes)

	// $schema 应被移除
	if strings.Contains(jsonStr, "$schema") {
		t.Error("$schema 未被清除")
	}
	// additionalProperties 应被移除
	if strings.Contains(jsonStr, "additionalProperties") {
		t.Error("additionalProperties 未被清除")
	}
	// 空 required 数组应被移除（chrome__screenshot 的 required 为空）
	// 非空 required 应保留
	if !strings.Contains(jsonStr, `"required"`) {
		t.Error("非空 required 不应被删除")
	}

	// ===== 验证历史消息结构 =====
	history := result.ConversationState.History

	// 检查 history 中并行 tool_use 是否正确转换
	foundParallelToolUse := false
	for _, msg := range history {
		if msg.AssistantResponseMessage != nil && len(msg.AssistantResponseMessage.ToolUses) == 2 {
			foundParallelToolUse = true
			tu0 := msg.AssistantResponseMessage.ToolUses[0]
			tu1 := msg.AssistantResponseMessage.ToolUses[1]
			if tu0.Name != "mcp__github__search_repositories" {
				t.Errorf("并行 tool_use[0] 名应为 mcp__github__search_repositories, got: %s", tu0.Name)
			}
			if tu1.Name != "mcp__ssh__execute_command" {
				t.Errorf("并行 tool_use[1] 名应为 mcp__ssh__execute_command, got: %s", tu1.Name)
			}
			break
		}
	}
	if !foundParallelToolUse {
		t.Error("history 中未找到包含 2 个并行 tool_use 的 assistant 消息")
	}

	// 检查 history 中 tool_result 匹配
	foundToolResult := false
	for _, msg := range history {
		if msg.UserInputMessage != nil && len(msg.UserInputMessage.UserInputMessageContext.ToolResults) == 2 {
			foundToolResult = true
			tr0 := msg.UserInputMessage.UserInputMessageContext.ToolResults[0]
			tr1 := msg.UserInputMessage.UserInputMessageContext.ToolResults[1]
			if tr0.ToolUseID != "mcp_call_001" || tr1.ToolUseID != "mcp_call_002" {
				t.Errorf("并行 toolResult ID 不匹配: %s, %s", tr0.ToolUseID, tr1.ToolUseID)
			}
			break
		}
	}
	if !foundToolResult {
		t.Error("history 中未找到包含 2 个并行 toolResult 的 user 消息")
	}

	// ===== 验证 currentMessage =====
	cm := result.ConversationState.CurrentMessage.UserInputMessage
	// 最后一条用户消息包含 tool_result + 文本
	if !strings.Contains(cm.Content, "帮我在这个项目上创建一个 issue") {
		t.Errorf("currentMessage 应包含用户文本, got: %s", cm.Content)
	}
	// 应有 screenshot 的 tool_result
	if len(cm.UserInputMessageContext.ToolResults) != 1 {
		t.Errorf("currentMessage 应有 1 个 toolResult, got: %d", len(cm.UserInputMessageContext.ToolResults))
	} else if cm.UserInputMessageContext.ToolResults[0].ToolUseID != "mcp_call_004" {
		t.Errorf("currentMessage toolResult ID 应为 mcp_call_004, got: %s",
			cm.UserInputMessageContext.ToolResults[0].ToolUseID)
	}

	t.Logf("MCP 工具请求转换成功 - 工具数: %d, 历史消息数: %d",
		len(tools), len(history))
}
