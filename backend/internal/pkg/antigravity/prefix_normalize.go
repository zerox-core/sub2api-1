package antigravity

// prefix_normalize.go — 稳定 Antigravity/Gemini 隐式缓存前缀的归一化处理。
//
// 背景：Antigravity 上游的隐式缓存按「请求前缀逐字节匹配」。Claude Code /
// Claude Desktop 会在历史消息里放置 <system-reminder> 块，其中包含日期、时间、
// git 状态等会随会话变动的内容；任何一处变化都会让前缀从该点之后全部失效，
// 表现为 cache_read 突然归零、整段上下文按全价计费（实测单会话可在 6 秒内从
// 85k 命中跌到 0）。
//
// 这里只做最小、可逆的归一化：
//   - 只处理「历史消息」（最后一条保持原样，当前轮的日期/时间仍对模型可见）；
//   - 只改写 <system-reminder> 块内的日期、时间戳与撇号变体；
//   - 不删除任何 block，不改变消息顺序，未命中的内容逐字节保持原样。
//
// 关闭方式：SUB2API_ANTIGRAVITY_PREFIX_NORMALIZE=0（或 false/off）。

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
)

var (
	reminderSpanRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	isoDateTimeRE  = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:?\d{2})?\b`)
	dateOnlyRE     = regexp.MustCompile(`\b\d{4}[-/]\d{2}[-/]\d{2}\b`)
	clockTimeRE    = regexp.MustCompile(`\b\d{1,2}:\d{2}(:\d{2})?\b`)
	apostropheRepl = strings.NewReplacer("’", "'", "ʼ", "'", "ʹ", "'")
)

var prefixNormalizeState struct {
	once sync.Once
	off  bool
}

func prefixNormalizeEnabled() bool {
	prefixNormalizeState.once.Do(func() {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("SUB2API_ANTIGRAVITY_PREFIX_NORMALIZE"))) {
		case "0", "false", "off", "no":
			prefixNormalizeState.off = true
		}
	})
	return !prefixNormalizeState.off
}

// NormalizeHistoricalReminders 归一化历史消息中 <system-reminder> 块的易变字段。
// 返回是否发生了改写。最后一条消息（当前轮）始终不做处理。
func NormalizeHistoricalReminders(req *ClaudeRequest) bool {
	if req == nil || len(req.Messages) < 2 || !prefixNormalizeEnabled() {
		return false
	}
	changed := false
	for i := 0; i < len(req.Messages)-1; i++ {
		msg := &req.Messages[i]
		if len(msg.Content) == 0 {
			continue
		}
		next, ok := normalizeMessageContent(msg.Content)
		if !ok {
			continue
		}
		msg.Content = next
		changed = true
	}
	return changed
}

func normalizeMessageContent(raw json.RawMessage) (json.RawMessage, bool) {
	// 字符串形式 content
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if !strings.Contains(text, "<system-reminder>") {
			return nil, false
		}
		next := normalizeReminderText(text)
		if next == text {
			return nil, false
		}
		out, err := json.Marshal(next)
		if err != nil {
			return nil, false
		}
		return out, true
	}

	// 数组形式 content：用 map 遍历以保留 cache_control 等未知字段。
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, false
	}
	changed := false
	for _, block := range blocks {
		if block == nil {
			continue
		}
		if kind, _ := block["type"].(string); kind != "text" {
			continue
		}
		text, _ := block["text"].(string)
		if text == "" || !strings.Contains(text, "<system-reminder>") {
			continue
		}
		if next := normalizeReminderText(text); next != text {
			block["text"] = next
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(blocks)
	if err != nil {
		return nil, false
	}
	return out, true
}

// normalizeReminderText 只改写 <system-reminder> 区间内的文本，其余原样返回。
func normalizeReminderText(text string) string {
	if !strings.Contains(text, "<system-reminder>") {
		return text
	}
	return reminderSpanRE.ReplaceAllStringFunc(text, normalizeReminderSpan)
}

func normalizeReminderSpan(span string) string {
	span = apostropheRepl.Replace(span)
	span = isoDateTimeRE.ReplaceAllString(span, "<datetime>")
	span = dateOnlyRE.ReplaceAllString(span, "<date>")
	span = clockTimeRE.ReplaceAllString(span, "<time>")
	return span
}
