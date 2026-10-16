package service

import (
	"encoding/json"
	"strings"
	"time"
)

// 账号级「上游明确不支持」模型清单（R5：账号层真实支持性过滤）。
//
// 背景：渠道目录里常混着该账号上游并不提供的模型（zyuou 目录没有 gpt-image-1、
// 丽珠没有 gpt-image-2.5-*）。这类模型一旦被调度到该账号，上游会稳定返回
// 404 model_not_found，客户端直接看到失败。
//
// 已有机制只做 30 分钟临时冷却（RateLimitService.HandleUpstreamModelNotFound +
// per-(账号,模型) rate limit）：冷却到期或进程重启后又会重新踩同一个坑，
// 而且管理端看不到「哪个账号到底吃哪些模型」。
//
// 这一层补的是持久记账，把「上游明确说过不支持」的模型写进 accounts.extra：
//   - 供 IsModelSupported 做账号层真实支持性过滤，调度不再选它；
//   - 供管理端查看与清除（渠道补上模型后可手动放行）；
//   - 计次 + 时间戳，达到阈值才生效，避免一次偶发 404 就永久拉黑。
const (
	// UnsupportedModelsExtraKey 是 accounts.extra 中承载该清单的键。
	UnsupportedModelsExtraKey = "unsupported_models"
	// unsupportedModelBlacklistThreshold 命中达到该次数才判定为「真实不支持」。
	unsupportedModelBlacklistThreshold = 2
	// unsupportedModelBlacklistTTL 记录超过该时长未再命中即失效，给上游修复留窗口。
	unsupportedModelBlacklistTTL = 7 * 24 * time.Hour
)

// UnsupportedModelRecord 记录上游明确拒绝某模型的事实。
type UnsupportedModelRecord struct {
	Status    int    `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
}

// AccountUnsupportedModels 是账号的「上游不支持模型」清单，键为归一化模型名。
type AccountUnsupportedModels map[string]UnsupportedModelRecord

// normalizeUnsupportedModelKey 归一化模型名，保证读/写两侧与调度判定一致。
func normalizeUnsupportedModelKey(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	name = strings.TrimPrefix(name, "models/")
	return strings.TrimSpace(name)
}

// NormalizeUnsupportedModelName 暴露归一化结果，供管理端按模型名放行单条记录时
// 用它对齐清单键（清单键一律是归一化后的名字）。
func NormalizeUnsupportedModelName(model string) string {
	return normalizeUnsupportedModelKey(model)
}

// UnsupportedModelRecords 解析账号上的不支持模型清单（无记录时返回 nil）。
func (a *Account) UnsupportedModelRecords() AccountUnsupportedModels {
	if a == nil || a.Extra == nil {
		return nil
	}
	raw, ok := a.Extra[UnsupportedModelsExtraKey]
	if !ok || raw == nil {
		return nil
	}
	return decodeUnsupportedModels(raw)
}

// decodeUnsupportedModels 容忍多种承载形态：extra 从数据库读出时是
// map[string]any，从代码写入时可能是本包类型或 JSON 字节。
func decodeUnsupportedModels(raw any) AccountUnsupportedModels {
	switch typed := raw.(type) {
	case AccountUnsupportedModels:
		return typed
	case nil:
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var decoded AccountUnsupportedModels
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) == 0 {
		return nil
	}
	return decoded
}

// unsupportedModelRecordEffective 判断记录是否已生效（达阈值且未过期）。
func unsupportedModelRecordEffective(record UnsupportedModelRecord, now time.Time, ttl time.Duration) bool {
	if record.Count < unsupportedModelBlacklistThreshold {
		return false
	}
	lastSeen, err := time.Parse(time.RFC3339, strings.TrimSpace(record.LastSeen))
	if err != nil {
		// 时间戳缺失或损坏时继续拉黑：宁可多排除一个模型，也不要再踩确定性 404。
		return true
	}
	return now.Sub(lastSeen) <= ttl
}

// IsModelMarkedUnsupported 报告该模型是否已被上游明确拒绝且记录仍在有效期内。
// 命中时账号不应再接收该模型的请求（IsModelSupported 会因此返回 false）。
func (a *Account) IsModelMarkedUnsupported(model string) bool {
	if a == nil || a.Extra == nil {
		return false
	}
	// 绝大多数账号没有该键，先做零分配快速路径（IsModelSupported 在调度热路径上）。
	if _, ok := a.Extra[UnsupportedModelsExtraKey]; !ok {
		return false
	}
	records := a.UnsupportedModelRecords()
	if len(records) == 0 {
		return false
	}
	key := normalizeUnsupportedModelKey(model)
	if key == "" {
		return false
	}
	record, ok := records[key]
	if !ok {
		return false
	}
	return unsupportedModelRecordEffective(record, time.Now(), unsupportedModelBlacklistTTL)
}

// pruneExpiredUnsupportedModels 丢掉早已失效的记录，避免 extra 无限增长。
func pruneExpiredUnsupportedModels(current AccountUnsupportedModels, now time.Time) AccountUnsupportedModels {
	if len(current) == 0 {
		return current
	}
	pruned := make(AccountUnsupportedModels, len(current))
	for key, record := range current {
		// 未达阈值说明还在观察期，保留计数；达阈值但过期则丢弃。
		if record.Count >= unsupportedModelBlacklistThreshold &&
			!unsupportedModelRecordEffective(record, now, unsupportedModelBlacklistTTL) {
			continue
		}
		pruned[key] = record
	}
	return pruned
}

// recordUnsupportedModel 把一次「上游明确不支持」合并进清单（纯函数，便于测试）。
func recordUnsupportedModel(
	current AccountUnsupportedModels,
	model string,
	status int,
	reason string,
	now time.Time,
) (AccountUnsupportedModels, bool) {
	key := normalizeUnsupportedModelKey(model)
	if key == "" {
		return current, false
	}
	updated := make(AccountUnsupportedModels, len(current)+1)
	for existingKey, record := range current {
		updated[existingKey] = record
	}
	timestamp := now.UTC().Format(time.RFC3339)
	record := updated[key]
	if record.Count == 0 {
		record.FirstSeen = timestamp
	}
	record.Count++
	record.Status = status
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		record.Reason = trimmed
	}
	record.LastSeen = timestamp
	updated[key] = record
	return pruneExpiredUnsupportedModels(updated, now), true
}

// UnsupportedModelExtraUpdate 生成写回 accounts.extra 的增量（保持仓库层 merge 语义）。
func UnsupportedModelExtraUpdate(records AccountUnsupportedModels) map[string]any {
	if len(records) == 0 {
		// 显式写 JSON null，让仓库层按既有约定删除该键（见 UpdateExtra 的 nil 处理）。
		return map[string]any{UnsupportedModelsExtraKey: nil}
	}
	return map[string]any{UnsupportedModelsExtraKey: records}
}
