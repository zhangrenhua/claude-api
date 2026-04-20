package stream

import (
	"testing"
)

func TestReplaceBrandingWithModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		model    string
		expected string
	}{
		// 基本替换：模型名/Kiro → 都被替换为传入的 modelName
		{"sonnet → opus-4-7", "I am Claude Sonnet", "claude-opus-4-7", "I am claude-opus-4-7"},
		{"versioned sonnet → opus-4-7", "I am Claude 3.5 Sonnet", "claude-opus-4-7", "I am claude-opus-4-7"},
		{"haiku → opus-4-7", "I am Claude Haiku", "claude-opus-4-7", "I am claude-opus-4-7"},
		// 带尾部版本号：应完整消耗，不留多余数字
		{"sonnet trailing ver", "I am Claude Sonnet 4.5", "claude-opus-4-7", "I am claude-opus-4-7"},
		{"opus trailing ver", "I am Claude Opus 4.5", "claude-opus-4-7", "I am claude-opus-4-7"},
		{"sonnet trailing int", "I am Claude Sonnet 4", "claude-opus-4-7", "I am claude-opus-4-7"},
		// 模型 ID 格式
		{"old model id", "claude-3-5-sonnet-20241022", "gpt-5.2-codex", "gpt-5.2-codex"},
		{"new model id", "claude-sonnet-4-5-20250929", "gpt-5.2-codex", "gpt-5.2-codex"},
		{"short model id", "claude-opus-4-6", "gpt-5.4-thinking", "gpt-5.4-thinking"},
		// Kiro 替换 → 也用 modelName
		{"kiro → claude-opus-4-7", "I am Kiro", "claude-opus-4-7", "I am claude-opus-4-7"},
		{"kiro → gpt-5.2", "Hi, I'm Kiro!", "gpt-5.2", "Hi, I'm gpt-5.2!"},
		// 不同 modelName 都能正确生效
		{"different model A", "I am Claude Opus", "model-A", "I am model-A"},
		{"different model B", "I am Claude Opus", "model-B", "I am model-B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceBrandingWithModel(tt.input, tt.model)
			if got != tt.expected {
				t.Errorf("ReplaceBrandingWithModel(%q, %q) = %q, want %q", tt.input, tt.model, got, tt.expected)
			}
		})
	}
}

func TestReplaceBrandInContent_StreamingPending(t *testing.T) {
	// 模拟流式场景：chunk 边界在品牌名中间
	// 确保 pending 被 flush 时不会叠加多余的数字
	const modelName = "claude-opus-4-7"

	// Chunk 1: "I am Claude" → pending="Claude"
	emit1, chars1, pending1 := replaceBrandInContent("I am Claude", 0, "", modelName)
	if pending1 == "" {
		t.Fatal("expected non-empty pending after 'I am Claude'")
	}

	// Chunk 2: " Sonnet 4.5 is" → 应替换为 "claude-opus-4-7 is"
	emit2, _, pending2 := replaceBrandInContent(" Sonnet 4.5 is", chars1, pending1, modelName)
	combined := emit1 + emit2 + pending2
	want := "I am claude-opus-4-7 is"
	if combined != want {
		t.Errorf("streaming result = %q, want %q", combined, want)
	}
}
