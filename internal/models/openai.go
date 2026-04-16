package models

// ToolCall 表示工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 表示函数调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatMessage 表示 OpenAI 聊天消息
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // 通常是字符串，但可以是多部分
	ToolCalls  interface{} `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// OpenAITool 表示工具定义，支持两种格式：
// 1. OpenAI 格式: {"type":"function", "function":{...}}
// 2. Claude 原生格式: {"name":"...", "description":"...", "input_schema": {...}}
type OpenAITool struct {
	// OpenAI 格式字段
	Type     string              `json:"type,omitempty"`
	Function *OpenAIToolFunction `json:"function,omitempty"`

	// Claude 原生格式字段
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`

	// Responses API 格式字段（工具参数在顶层）
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// OpenAIToolFunction 表示函数定义
type OpenAIToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ChatCompletionRequest 表示 OpenAI 聊天完成请求
type ChatCompletionRequest struct {
	Model               string        `json:"model"`
	Messages            []ChatMessage `json:"messages" binding:"required"`
	Stream              bool          `json:"stream,omitempty"`
	Tools               []OpenAITool  `json:"tools,omitempty"`
	MaxTokens           int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Temperature         *float64      `json:"temperature,omitempty"`
	TopP                *float64      `json:"top_p,omitempty"`
	ToolChoice          interface{}   `json:"tool_choice,omitempty"`
}

// ChatCompletionResponse 表示非流式 OpenAI 响应
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
}

// ChatCompletionChoice 表示响应中的选项
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatCompletionUsage 表示令牌使用情况
type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk 表示流式块
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *ChatCompletionUsage        `json:"usage,omitempty"`
}

// ChatCompletionChunkChoice 表示流式块中的选项
type ChatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason *string                  `json:"finish_reason"`
}

// ChatCompletionChunkDelta 表示流式块中的增量
type ChatCompletionChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ResponsesRequest 表示 OpenAI Responses API 请求
type ResponsesRequest struct {
	Model           string       `json:"model"`
	Input           interface{}  `json:"input"`
	Instructions    string       `json:"instructions,omitempty"`
	Stream          bool         `json:"stream,omitempty"`
	Tools           []OpenAITool `json:"tools,omitempty"`
	ToolChoice      interface{}  `json:"tool_choice,omitempty"`
	MaxOutputTokens int          `json:"max_output_tokens,omitempty"`
	Temperature     *float64     `json:"temperature,omitempty"`
	TopP            *float64     `json:"top_p,omitempty"`
	Reasoning       interface{}  `json:"reasoning,omitempty"` // {"effort":"high"} → Claude thinking
}

