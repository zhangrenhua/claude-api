package api

import (
	"fmt"
	"strings"
	"testing"

	"claude-api/internal/models"
	"claude-api/internal/tokenizer"
)

// ==================== Tokenizer 基础测试 ====================

// TestTokenizerBasic 测试 tokenizer 基础功能
func TestTokenizerBasic(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		minToken int
		maxToken int
	}{
		// 空字符串
		{"空字符串", "", 0, 0},
		{"只有空格", "   ", 1, 3},
		{"只有换行", "\n\n\n", 1, 5},

		// 英文
		{"英文单词", "hello", 1, 2},
		{"英文短句", "Hello, world!", 3, 6},
		{"英文长句", "The quick brown fox jumps over the lazy dog.", 9, 12},
		{"英文段落", "Machine learning is a subset of artificial intelligence. It enables systems to learn from data.", 15, 25},

		// 中文
		{"中文单字", "你", 1, 3},
		{"中文词语", "你好", 2, 4},
		{"中文短句", "你好世界", 4, 8},
		{"中文长句", "今天天气真好，我们一起去公园散步吧。", 15, 35},
		{"中文段落", "人工智能是计算机科学的一个分支，它企图了解智能的实质，并生产出一种新的能以人类智能相似的方式做出反应的智能机器。", 40, 80},

		// 混合中英文
		{"混合短句", "Hello 你好 World 世界", 6, 12},
		{"混合长句", "Claude is an AI assistant. Claude 是一个 AI 助手。", 12, 25},

		// 数字
		{"纯数字", "1234567890", 1, 5},
		{"带小数", "3.14159265358979", 3, 20},
		{"数学表达式", "2 + 2 = 4", 5, 10},
		{"复杂数学", "∫(x²+2x+1)dx = x³/3 + x² + x + C", 15, 35},

		// 特殊字符
		{"标点符号", "!@#$%^&*()", 5, 15},
		{"括号", "((()))", 1, 10},
		{"引号", `"Hello" 'World'`, 4, 10},

		// Emoji 和 Unicode
		{"Emoji", "😀😁😂🤣", 4, 20},
		{"Unicode", "αβγδ", 4, 12},
		{"日文", "こんにちは", 5, 15},
		{"韩文", "안녕하세요", 5, 15},
	}

	fmt.Println("\n=== Tokenizer 基础测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountTokens(tt.text)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %q → %d tokens (预期: %d-%d)\n", status, tt.name, truncate(tt.text, 20), tokens, tt.minToken, tt.maxToken)
		})
	}
}

// TestTokenizerCode 测试代码片段的 token 计算
func TestTokenizerCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		minToken int
		maxToken int
	}{
		{
			"Go 函数",
			`func main() {
	fmt.Println("Hello, World!")
}`,
			10, 25,
		},
		{
			"Python 函数",
			`def hello():
    print("Hello, World!")`,
			8, 20,
		},
		{
			"JavaScript 函数",
			`function hello() {
    console.log("Hello, World!");
}`,
			10, 25,
		},
		{
			"SQL 查询",
			`SELECT * FROM users WHERE id = 1 AND status = 'active'`,
			12, 25,
		},
		{
			"JSON 数据",
			`{"name": "test", "value": 123, "active": true}`,
			15, 30,
		},
		{
			"HTML 标签",
			`<div class="container"><p>Hello</p></div>`,
			10, 25,
		},
	}

	fmt.Println("\n=== 代码片段 Token 测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountTokens(tt.code)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 输入 Token 计算测试 ====================

// TestCountClaudeInputTokens 测试 Claude 输入 token 计算
func TestCountClaudeInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		req      *models.ClaudeRequest
		minToken int
		maxToken int
	}{
		{
			name: "简单文本消息",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hello, how are you?"},
				},
			},
			minToken: 5,
			maxToken: 15,
		},
		{
			name: "带 system prompt",
			req: &models.ClaudeRequest{
				System: "You are a helpful assistant.",
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			minToken: 10,
			maxToken: 25,
		},
		{
			name: "长 system prompt",
			req: &models.ClaudeRequest{
				System: "You are a helpful AI assistant. You should always be polite, accurate, and helpful. Never provide harmful information.",
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hi"},
				},
			},
			minToken: 25,
			maxToken: 45,
		},
		{
			name: "2轮对话",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "What is 2+2?"},
					{Role: "assistant", Content: "2+2 equals 4."},
				},
			},
			minToken: 12,
			maxToken: 25,
		},
		{
			name: "5轮对话",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there!"},
					{Role: "user", Content: "What is AI?"},
					{Role: "assistant", Content: "AI stands for Artificial Intelligence."},
					{Role: "user", Content: "Thanks!"},
				},
			},
			minToken: 25,
			maxToken: 50,
		},
		{
			name: "10轮对话",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hi"},
					{Role: "assistant", Content: "Hello!"},
					{Role: "user", Content: "How are you?"},
					{Role: "assistant", Content: "I'm doing well, thanks!"},
					{Role: "user", Content: "What can you do?"},
					{Role: "assistant", Content: "I can help with many tasks."},
					{Role: "user", Content: "Like what?"},
					{Role: "assistant", Content: "Coding, writing, analysis, and more."},
					{Role: "user", Content: "Great!"},
					{Role: "assistant", Content: "How can I help you today?"},
				},
			},
			minToken: 50,
			maxToken: 100,
		},
		{
			name: "中文消息",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "你好，请帮我写一段代码"},
				},
			},
			minToken: 8,
			maxToken: 25,
		},
		{
			name: "代码内容",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "```go\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}\n```"},
				},
			},
			minToken: 15,
			maxToken: 40,
		},
		{
			name: "超长消息",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: strings.Repeat("This is a test message. ", 100)},
				},
			},
			minToken: 400,
			maxToken: 650,
		},
		{
			name: "空消息",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: ""},
				},
			},
			minToken: 0,
			maxToken: 10,
		},
	}

	fmt.Println("\n=== Claude 输入 Token 测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _, _ := countClaudeInputTokens(tt.req)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// TestCountOpenAIInputTokens 测试 OpenAI 格式输入 token 计算
func TestCountOpenAIInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		req      *models.ChatCompletionRequest
		minToken int
		maxToken int
	}{
		{
			name: "简单消息",
			req: &models.ChatCompletionRequest{
				Messages: []models.ChatMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			minToken: 3,
			maxToken: 10,
		},
		{
			name: "带 system 消息",
			req: &models.ChatCompletionRequest{
				Messages: []models.ChatMessage{
					{Role: "system", Content: "You are a helpful assistant."},
					{Role: "user", Content: "Hello"},
				},
			},
			minToken: 10,
			maxToken: 25,
		},
		{
			name: "多轮对话",
			req: &models.ChatCompletionRequest{
				Messages: []models.ChatMessage{
					{Role: "system", Content: "You are a helpful assistant."},
					{Role: "user", Content: "What is the capital of France?"},
					{Role: "assistant", Content: "The capital of France is Paris."},
					{Role: "user", Content: "What about Germany?"},
				},
			},
			minToken: 25,
			maxToken: 60,
		},
		{
			name: "中文对话",
			req: &models.ChatCompletionRequest{
				Messages: []models.ChatMessage{
					{Role: "user", Content: "你好，请问今天天气怎么样？"},
				},
			},
			minToken: 10,
			maxToken: 30,
		},
	}

	fmt.Println("\n=== OpenAI 输入 Token 测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := countOpenAIInputTokens(tt.req)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 输出 Token 计算测试 ====================

// TestOutputTokenCalculation 测试输出 token 计算
func TestOutputTokenCalculation(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		minToken int
		maxToken int
	}{
		// 短回复
		{"1个词", "OK", 1, 2},
		{"简短回答", "Yes, that's correct.", 4, 8},

		// 中等回复
		{"一句话", "The answer to your question is 42.", 7, 12},
		{"两句话", "I understand your question. Let me help you with that.", 10, 18},

		// 长回复
		{"段落回复", "Machine learning is a subset of artificial intelligence that enables systems to learn and improve from experience without being explicitly programmed. It focuses on developing algorithms that can access data and use it to learn for themselves.", 35, 55},

		// 代码回复
		{"简单代码", "```python\nprint('Hello')\n```", 8, 18},
		{"复杂代码", "```go\nfunc main() {\n    for i := 0; i < 10; i++ {\n        fmt.Println(i)\n    }\n}\n```", 25, 50},

		// 中文回复
		{"中文短回复", "好的，我明白了。", 6, 15},
		{"中文长回复", "这是一个测试回复，用于验证中文token计算是否正确。我们需要确保tokenizer能够准确处理中文字符。", 25, 60},

		// 混合回复
		{"混合回复", "The answer is 42. 答案是42。", 8, 18},
	}

	fmt.Println("\n=== 输出 Token 计算测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountTokens(tt.content)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 边界情况测试 ====================

// TestTokenizerEdgeCases 测试边界情况
func TestTokenizerEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		minToken int
		maxToken int
	}{
		{"空字符串", "", 0, 0},
		{"单个空格", " ", 1, 2},
		{"多个空格", "     ", 1, 5},
		{"制表符", "\t\t\t", 1, 5},
		{"换行符", "\n\n\n", 1, 5},
		{"混合空白", " \t\n ", 1, 6},
		{"超长单词", "supercalifragilisticexpialidocious", 5, 40},
		{"重复字符", strings.Repeat("a", 100), 5, 110},
		{"重复单词", strings.Repeat("hello ", 50), 50, 100},
		{"特殊 Unicode", "\u200B\u200C\u200D", 1, 10}, // 零宽字符
		{"控制字符", "\x00\x01\x02", 1, 10},
	}

	fmt.Println("\n=== 边界情况测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountTokens(tt.text)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 输入输出比例测试 ====================

// TestInputOutputRatio 测试输入输出比例是否合理
func TestInputOutputRatio(t *testing.T) {
	scenarios := []struct {
		name     string
		input    string
		output   string
		maxRatio float64
	}{
		{
			name:     "简单问答",
			input:    "What is 2+2?",
			output:   "2+2 equals 4.",
			maxRatio: 3.0,
		},
		{
			name:     "代码生成",
			input:    "Write a hello world function in Go",
			output:   "```go\nfunc hello() {\n    fmt.Println(\"Hello, World!\")\n}\n```",
			maxRatio: 2.0,
		},
		{
			name:     "长问题短回答",
			input:    "Please analyze the following complex mathematical equation and provide a simple yes or no answer: is x^2 + 2x + 1 = (x+1)^2 a valid identity?",
			output:   "Yes",
			maxRatio: 50.0,
		},
		{
			name:     "短问题长回答",
			input:    "Explain AI",
			output:   "Artificial Intelligence (AI) is a branch of computer science that aims to create intelligent machines that can perform tasks that typically require human intelligence. This includes learning, reasoning, problem-solving, perception, and language understanding.",
			maxRatio: 0.5,
		},
		{
			name:     "中文问答",
			input:    "什么是人工智能？",
			output:   "人工智能是计算机科学的一个分支，旨在创建能够执行通常需要人类智能的任务的智能机器。",
			maxRatio: 1.5,
		},
	}

	fmt.Println("\n=== 输入输出比例测试 ===")
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			inputTokens := tokenizer.CountTokens(s.input)
			outputTokens := tokenizer.CountTokens(s.output)
			ratio := float64(inputTokens) / float64(outputTokens)

			status := "✓"
			if ratio > s.maxRatio {
				status = "✗"
				t.Errorf("比例 %.2f 超过最大允许值 %.2f", ratio, s.maxRatio)
			}
			fmt.Printf("%s %s: 输入=%d, 输出=%d, 比例=%.2f (最大: %.2f)\n",
				status, s.name, inputTokens, outputTokens, ratio, s.maxRatio)
		})
	}
}

// ==================== 性能基准测试 ====================

// BenchmarkTokenizer 测试 tokenizer 性能
func BenchmarkTokenizer(b *testing.B) {
	texts := []struct {
		name string
		text string
	}{
		{"短文本", "Hello, world!"},
		{"中等文本", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 10)},
		{"长文本", strings.Repeat("Machine learning is a subset of artificial intelligence. ", 100)},
		{"中文文本", strings.Repeat("人工智能是计算机科学的一个分支。", 50)},
		{"代码文本", strings.Repeat("func main() { fmt.Println(\"Hello\") }\n", 50)},
	}

	for _, tt := range texts {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tokenizer.CountTokens(tt.text)
			}
		})
	}
}

// ==================== 辅助函数 ====================

// ==================== CountMessageTokens 测试 ====================

// TestCountMessageTokens 测试 tokenizer.CountMessageTokens 函数
func TestCountMessageTokens(t *testing.T) {
	tests := []struct {
		name         string
		messages     []interface{}
		systemPrompt string
		minToken     int
		maxToken     int
	}{
		{
			name:         "空消息无 system",
			messages:     []interface{}{},
			systemPrompt: "",
			minToken:     0,
			maxToken:     0,
		},
		{
			name:         "仅 system prompt",
			messages:     []interface{}{},
			systemPrompt: "You are a helpful assistant.",
			minToken:     5,
			maxToken:     15,
		},
		{
			name: "单条用户消息",
			messages: []interface{}{
				map[string]interface{}{"role": "user", "content": "Hello"},
			},
			systemPrompt: "",
			minToken:     5,
			maxToken:     15,
		},
		{
			name: "带 system 的单条消息",
			messages: []interface{}{
				map[string]interface{}{"role": "user", "content": "Hello"},
			},
			systemPrompt: "You are a helpful assistant.",
			minToken:     10,
			maxToken:     25,
		},
		{
			name: "多轮对话",
			messages: []interface{}{
				map[string]interface{}{"role": "user", "content": "What is AI?"},
				map[string]interface{}{"role": "assistant", "content": "AI stands for Artificial Intelligence."},
				map[string]interface{}{"role": "user", "content": "Thanks!"},
			},
			systemPrompt: "",
			minToken:     20,
			maxToken:     45,
		},
		{
			name: "内容块格式",
			messages: []interface{}{
				map[string]interface{}{
					"role": "user",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Hello world"},
					},
				},
			},
			systemPrompt: "",
			minToken:     5,
			maxToken:     15,
		},
	}

	fmt.Println("\n=== CountMessageTokens 测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountMessageTokens(tt.messages, tt.systemPrompt)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== CountToolTokens 测试 ====================

// TestCountToolTokens 测试工具定义的 token 计算
func TestCountToolTokens(t *testing.T) {
	tests := []struct {
		name     string
		tools    []interface{}
		minToken int
		maxToken int
	}{
		{
			name:     "空工具列表",
			tools:    []interface{}{},
			minToken: 0,
			maxToken: 0,
		},
		{
			name: "单个简单工具",
			tools: []interface{}{
				map[string]interface{}{
					"name":        "get_weather",
					"description": "Get weather information",
				},
			},
			minToken: 10,
			maxToken: 30,
		},
		{
			name: "带参数的工具",
			tools: []interface{}{
				map[string]interface{}{
					"name":        "search",
					"description": "Search for information",
					"input_schema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "Search query",
							},
						},
						"required": []string{"query"},
					},
				},
			},
			minToken: 30,
			maxToken: 80,
		},
		{
			name: "多个工具",
			tools: []interface{}{
				map[string]interface{}{
					"name":        "tool1",
					"description": "First tool",
				},
				map[string]interface{}{
					"name":        "tool2",
					"description": "Second tool",
				},
				map[string]interface{}{
					"name":        "tool3",
					"description": "Third tool",
				},
			},
			minToken: 25,
			maxToken: 70,
		},
	}

	fmt.Println("\n=== CountToolTokens 测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountToolTokens(tt.tools)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 复杂内容块测试 ====================

// TestComplexContentBlocks 测试复杂内容块的 token 计算
func TestComplexContentBlocks(t *testing.T) {
	tests := []struct {
		name     string
		req      *models.ClaudeRequest
		minToken int
		maxToken int
	}{
		{
			name: "文本内容块",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{
						Role: "user",
						Content: []interface{}{
							map[string]interface{}{"type": "text", "text": "Hello, world!"},
						},
					},
				},
			},
			minToken: 5,
			maxToken: 15,
		},
		{
			name: "多个文本块",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{
						Role: "user",
						Content: []interface{}{
							map[string]interface{}{"type": "text", "text": "First part."},
							map[string]interface{}{"type": "text", "text": "Second part."},
						},
					},
				},
			},
			minToken: 8,
			maxToken: 20,
		},
		{
			name: "混合字符串和内容块消息",
			req: &models.ClaudeRequest{
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Simple text message"},
					{
						Role: "assistant",
						Content: []interface{}{
							map[string]interface{}{"type": "text", "text": "Response with blocks"},
						},
					},
				},
			},
			minToken: 10,
			maxToken: 25,
		},
	}

	fmt.Println("\n=== 复杂内容块测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _, _ := countClaudeInputTokens(tt.req)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== System Prompt 变体测试 ====================

// TestSystemPromptVariants 测试不同类型的 system prompt
func TestSystemPromptVariants(t *testing.T) {
	tests := []struct {
		name     string
		req      *models.ClaudeRequest
		minToken int
		maxToken int
	}{
		{
			name: "字符串 system",
			req: &models.ClaudeRequest{
				System: "You are a helpful assistant.",
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hi"},
				},
			},
			minToken: 8,
			maxToken: 20,
		},
		{
			name: "空 system",
			req: &models.ClaudeRequest{
				System: "",
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hi"},
				},
			},
			minToken: 3,
			maxToken: 10,
		},
		{
			name: "长 system prompt",
			req: &models.ClaudeRequest{
				System: strings.Repeat("You are a helpful AI assistant. ", 20),
				Messages: []models.ClaudeMessage{
					{Role: "user", Content: "Hi"},
				},
			},
			minToken: 100,
			maxToken: 180,
		},
	}

	fmt.Println("\n=== System Prompt 变体测试 ===")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _, _ := countClaudeInputTokens(tt.req)
			status := "✓"
			if tokens < tt.minToken || tokens > tt.maxToken {
				status = "✗"
				t.Errorf("Token数 %d 不在预期范围 [%d, %d] 内", tokens, tt.minToken, tt.maxToken)
			}
			fmt.Printf("%s %s: %d tokens (预期: %d-%d)\n", status, tt.name, tokens, tt.minToken, tt.maxToken)
		})
	}
}

// ==================== 一致性测试 ====================

// TestTokenCountConsistency 测试 token 计算的一致性
func TestTokenCountConsistency(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog."

	fmt.Println("\n=== Token 计算一致性测试 ===")

	// 多次计算同一文本，结果应该一致
	results := make([]int, 10)
	for i := 0; i < 10; i++ {
		results[i] = tokenizer.CountTokens(text)
	}

	first := results[0]
	for i, r := range results {
		if r != first {
			t.Errorf("第 %d 次计算结果 %d 与第一次 %d 不一致", i+1, r, first)
		}
	}
	fmt.Printf("✓ 10次计算结果一致: %d tokens\n", first)

	// CountTokens 和 CountTokensForClaude 应该返回相同结果
	count1 := tokenizer.CountTokens(text)
	count2 := tokenizer.CountTokensForClaude(text)
	if count1 != count2 {
		t.Errorf("CountTokens(%d) != CountTokensForClaude(%d)", count1, count2)
	}
	fmt.Printf("✓ CountTokens 和 CountTokensForClaude 结果一致: %d tokens\n", count1)
}

// ==================== 辅助函数 ====================

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
