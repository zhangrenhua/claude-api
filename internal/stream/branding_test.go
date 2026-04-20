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
		// gpt / ChatGPT 身份串（上游漂移时的兜底替换）
		{"gpt-5.4 → opus", "我是 gpt-5.4，一个 AI 助手", "claude-opus-4-6", "我是 claude-opus-4-6，一个 AI 助手"},
		{"gpt-5 → opus", "I am gpt-5, nice to meet", "claude-opus-4-6", "I am claude-opus-4-6, nice to meet"},
		{"gpt-5-codex → opus", "gpt-5-codex here", "claude-opus-4-6", "claude-opus-4-6 here"},
		{"gpt-5.4-thinking → opus", "Hi gpt-5.4-thinking", "claude-opus-4-6", "Hi claude-opus-4-6"},
		{"GPT-5 uppercase", "GPT-5 ready", "claude-opus-4-6", "claude-opus-4-6 ready"},
		{"chatgpt space ver", "I am ChatGPT 5", "claude-opus-4-6", "I am claude-opus-4-6"},
		{"chatgpt dash ver", "I am ChatGPT-5", "claude-opus-4-6", "I am claude-opus-4-6"},
		{"chatgpt bare", "Hello from ChatGPT!", "claude-opus-4-6", "Hello from claude-opus-4-6!"},
		// 不应误伤非身份串
		{"no false positive gpt word", "gpt is an acronym", "claude-opus-4-6", "gpt is an acronym"},
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

// 基准场景：对典型 200 字符开头文本做完整替换管道（含 Kiro/Claude/gpt/ChatGPT 四类命中）
var benchModel = "claude-opus-4-6"

var benchHitText = "我是 Kiro，也有人叫我 Claude Sonnet 4.5 或 claude-sonnet-4-5-20250929，甚至 ChatGPT 5 / gpt-5.4-thinking。" +
	"一个专为开发者打造的 AI 助手和 IDE，可以帮你编写代码、分析项目、调试测试、处理基础设施配置。"

var benchMissText = "The quick brown fox jumps over the lazy dog. " +
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
	"Ut enim ad minim veniam, quis nostrud exercitation."

func BenchmarkReplaceBrandingWithModel_Hit(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ReplaceBrandingWithModel(benchHitText, benchModel)
	}
}

func BenchmarkReplaceBrandingWithModel_Miss(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ReplaceBrandingWithModel(benchMissText, benchModel)
	}
}

func BenchmarkReplaceBrandInContent_StreamingChunks(b *testing.B) {
	// 模拟典型流式场景：20 个短 chunk，共 ~200 字符，分散命中
	chunks := []string{
		"我是 ", "Ki", "ro", "，一个", "专为", "开发者", "打造的 ",
		"AI 助手", "和 IDE。", "也可以", "叫我 ", "Claude ", "Sonnet ",
		"4.5 或 ", "gpt-", "5.4", "-thinking", "，ChatGPT", " 5 ",
		"也行。",
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chars := 0
		pending := ""
		for _, c := range chunks {
			_, chars, pending = replaceBrandInContent(c, chars, pending, benchModel)
		}
		_ = pending
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
