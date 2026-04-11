package stream

import (
	"testing"
)

func TestReplaceBranding(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 基本替换
		{"sonnet", "I am Claude Sonnet", "I am Claude Opus 4"},
		{"versioned sonnet", "I am Claude 3.5 Sonnet", "I am Claude Opus 4"},
		{"haiku", "I am Claude Haiku", "I am Claude Opus 4"},
		// 带尾部版本号：应完整消耗，不留多余数字
		{"sonnet trailing ver", "I am Claude Sonnet 4.5", "I am Claude Opus 4"},
		{"opus trailing ver", "I am Claude Opus 4.5", "I am Claude Opus 4"},
		{"sonnet trailing int", "I am Claude Sonnet 4", "I am Claude Opus 4"},
		// 幂等性：已替换的文本再次替换不变
		{"idempotent", "I am Claude Opus 4", "I am Claude Opus 4"},
		{"idempotent with ctx", "I am Claude Opus 4, nice!", "I am Claude Opus 4, nice!"},
		{"idempotent sentence", "Claude Opus 4 is great", "Claude Opus 4 is great"},
		// 模型 ID 格式
		{"old model id", "claude-3-5-sonnet-20241022", "Claude Opus 4"},
		{"new model id", "claude-sonnet-4-5-20250929", "Claude Opus 4"},
		{"short model id", "claude-opus-4-6", "Claude Opus 4"},
		// Kiro 替换
		{"kiro", "I am Kiro", "I am Claude"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceBranding(tt.input)
			if got != tt.expected {
				t.Errorf("ReplaceBranding(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			// 幂等性检查：再次替换结果应不变
			got2 := ReplaceBranding(got)
			if got2 != got {
				t.Errorf("NOT idempotent: ReplaceBranding(%q) = %q, then ReplaceBranding(%q) = %q",
					tt.input, got, got, got2)
			}
		})
	}
}

func TestReplaceBranding_StreamingPending(t *testing.T) {
	// 模拟流式场景：chunk 边界在品牌名中间
	// 确保 pending 被 flush 时不会叠加多余的 "4"

	// Chunk 1: "I am Claude" → pending="Claude"
	emit1, chars1, pending1 := replaceKiroInContent("I am Claude", 0, "")
	if pending1 == "" {
		t.Fatal("expected non-empty pending after 'I am Claude'")
	}

	// Chunk 2: " Sonnet 4.5 is" → 应替换为 "Claude Opus 4 is"
	emit2, _, pending2 := replaceKiroInContent(" Sonnet 4.5 is", chars1, pending1)
	combined := emit1 + emit2 + pending2
	if got := combined; got != "I am Claude Opus 4 is" {
		t.Errorf("streaming result = %q, want %q", got, "I am Claude Opus 4 is")
	}
}
