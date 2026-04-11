package claude

import (
	"claude-api/internal/models"
	"encoding/json"
	"testing"
)

// TestEndToEnd_JSONFormat 端到端验证生成的 JSON 符合上游 API 格式
func TestEndToEnd_JSONFormat(t *testing.T) {
	req := &models.ClaudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 4096,
		System:    "You are a helpful assistant.",
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: "Hello"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Let me help."},
					map[string]interface{}{
						"type": "tool_use", "id": "t1", "name": "Bash",
						"input": map[string]interface{}{"command": "ls"},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type": "tool_result", "tool_use_id": "t1",
						"content": "file1.txt\nfile2.txt",
					},
				},
			},
		},
		Tools: []models.ClaudeTool{
			{
				Name:        "Bash",
				Description: "Run a command",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":                 "string",
							"description":          "The command",
							"additionalProperties": false,
						},
					},
					"required":             []interface{}{"command"},
					"additionalProperties": false,
				},
			},
		},
	}

	result, err := ConvertClaudeToAmazonQ(req, "test-conv-123", false)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	cs := raw["conversationState"].(map[string]interface{})
	history := cs["history"].([]interface{})

	// 1. 验证 history 无顶层 messageId
	for i, entry := range history {
		entryMap := entry.(map[string]interface{})
		if _, has := entryMap["messageId"]; has {
			t.Errorf("history[%d] 不应有顶层 messageId", i)
		}
	}

	// 2. 验证 assistant 消息无空 messageId
	for i, entry := range history {
		entryMap := entry.(map[string]interface{})
		if arm, ok := entryMap["assistantResponseMessage"].(map[string]interface{}); ok {
			if mid, has := arm["messageId"]; has {
				if mid == "" {
					t.Errorf("history[%d] assistant messageId 不应为空字符串", i)
				}
			}
		}
	}

	// 3. 验证工具 schema 无 additionalProperties（递归检查）
	curMsg := cs["currentMessage"].(map[string]interface{})
	uim := curMsg["userInputMessage"].(map[string]interface{})
	ctx := uim["userInputMessageContext"].(map[string]interface{})

	if tools, ok := ctx["tools"].([]interface{}); ok {
		for _, tool := range tools {
			spec := tool.(map[string]interface{})["toolSpecification"].(map[string]interface{})
			schema := spec["inputSchema"].(map[string]interface{})["json"].(map[string]interface{})
			checkNoAdditionalProperties(t, spec["name"].(string), schema, "")
		}
	}

	// 4. 验证 envState 始终存在
	if _, has := ctx["envState"]; !has {
		t.Error("currentMessage 应有 envState")
	}

	// 5. 验证 history user messages 有 envState
	for i, entry := range history {
		entryMap := entry.(map[string]interface{})
		if uim, ok := entryMap["userInputMessage"].(map[string]interface{}); ok {
			if uctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
				if _, has := uctx["envState"]; !has {
					t.Errorf("history[%d] user message 应有 envState", i)
				}
			}
		}
	}

	// 6. 验证 chatTriggerType
	if cs["chatTriggerType"] != "MANUAL" {
		t.Errorf("chatTriggerType 应为 MANUAL, got: %v", cs["chatTriggerType"])
	}

	// 7. 验证 currentMessage 有 toolResults
	if trs, ok := ctx["toolResults"].([]interface{}); ok {
		if len(trs) != 1 {
			t.Errorf("应有 1 个 toolResult, got: %d", len(trs))
		}
	} else {
		t.Error("currentMessage 应有 toolResults")
	}
}

func checkNoAdditionalProperties(t *testing.T, toolName string, schema map[string]interface{}, path string) {
	t.Helper()
	if _, has := schema["additionalProperties"]; has {
		t.Errorf("tool %s: schema%s 不应有 additionalProperties", toolName, path)
	}
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for name, val := range props {
			if propSchema, ok := val.(map[string]interface{}); ok {
				checkNoAdditionalProperties(t, toolName, propSchema, path+"."+name)
			}
		}
	}
}
