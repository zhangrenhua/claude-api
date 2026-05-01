package stream

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitContentWithThinking_UserReportedCase(t *testing.T) {
	input := "<thinking>\nThe user is asking about my knowledge cutoff date in Chinese.\n\nAccording to the system prompt, I should respond in Chinese.\n</thinking>\n\n我的知识库截止日期是 2026 年 1 月。"

	blocks := SplitContentWithThinking(input)
	out, _ := json.MarshalIndent(blocks, "", "  ")
	t.Logf("Output:\n%s", string(out))

	if len(blocks) != 2 {
		t.Fatalf("应该返回 2 个 block, 实际 %d", len(blocks))
	}
	if blocks[0]["type"] != "thinking" {
		t.Errorf("第一个 block 应该是 thinking, 实际 %v", blocks[0]["type"])
	}
	thinking, _ := blocks[0]["thinking"].(string)
	if !strings.Contains(thinking, "The user is asking") {
		t.Errorf("thinking 内容缺失")
	}
	if blocks[1]["type"] != "text" {
		t.Errorf("第二个 block 应该是 text, 实际 %v", blocks[1]["type"])
	}
	if text, _ := blocks[1]["text"].(string); !strings.Contains(text, "2026") {
		t.Errorf("text 内容缺失: %v", text)
	}
}

func TestSplitContentWithThinking_NoThinking(t *testing.T) {
	blocks := SplitContentWithThinking("Hello world")
	if len(blocks) != 1 || blocks[0]["type"] != "text" {
		t.Errorf("应该返回单个 text block")
	}
}

func TestSplitContentWithThinking_EmptyText(t *testing.T) {
	blocks := SplitContentWithThinking("")
	if len(blocks) != 0 {
		t.Errorf("空输入应该返回空数组, 实际 %d", len(blocks))
	}
}

func TestSplitContentWithThinking_MultipleBlocks(t *testing.T) {
	input := "<thinking>A</thinking>\n\nfirst<thinking>B</thinking>\n\nsecond"
	blocks := SplitContentWithThinking(input)
	if len(blocks) != 4 {
		t.Fatalf("应该返回 4 个 block, 实际 %d: %+v", len(blocks), blocks)
	}
}

func TestSplitContentWithThinking_QuotedTagInThinking(t *testing.T) {
	// 模型在 thinking 内提到 `</thinking>` 字面量（被反引号包裹），不应被误判为结束
	input := "<thinking>I'll close it with `</thinking>` later.\nReal end now.\n</thinking>\n\nfinal answer"
	blocks := SplitContentWithThinking(input)
	if len(blocks) != 2 {
		t.Fatalf("应该返回 2 个 block, 实际 %d: %+v", len(blocks), blocks)
	}
	thinking, _ := blocks[0]["thinking"].(string)
	if !strings.Contains(thinking, "Real end now") {
		t.Errorf("thinking 应包含真实结尾内容, 实际: %s", thinking)
	}
}
