package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func unsupportedRecordAccount(records AccountUnsupportedModels) *Account {
	return &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{UnsupportedModelsExtraKey: records},
	}
}

func TestRecordUnsupportedModelNeedsThreshold(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	records, changed := recordUnsupportedModel(nil, "gpt-image-1", 404, "upstream_404_model_not_found", now)
	require.True(t, changed)
	require.Equal(t, 1, records["gpt-image-1"].Count)

	// 首次命中只记数，不算"真实不支持"，避免一次偶发 404 就永久拉黑。
	account := unsupportedRecordAccount(records)
	require.False(t, account.IsModelMarkedUnsupported("gpt-image-1"))

	// 第二次命中达阈值才生效。
	records, _ = recordUnsupportedModel(records, "GPT-Image-1", 404, "upstream_404_model_not_found", now.Add(time.Minute))
	account = unsupportedRecordAccount(records)
	require.Equal(t, 2, records["gpt-image-1"].Count)
	require.True(t, account.IsModelMarkedUnsupported("gpt-image-1"))
	require.True(t, account.IsModelMarkedUnsupported("models/gpt-image-1"))
	require.True(t, account.IsModelMarkedUnsupported("  gpt-image-1  "))
	require.False(t, account.IsModelMarkedUnsupported("gpt-image-2"))
	require.False(t, account.IsModelMarkedUnsupported(""))
}

func TestUnsupportedModelRecordExpiresAfterTTL(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	records, _ := recordUnsupportedModel(nil, "gpt-image-1", 404, "", now)
	records, _ = recordUnsupportedModel(records, "gpt-image-1", 404, "", now)
	record := records["gpt-image-1"]

	require.True(t, unsupportedModelRecordEffective(record, now.Add(unsupportedModelBlacklistTTL-time.Hour), unsupportedModelBlacklistTTL))
	// 超过 TTL 未再命中 → 失效，给上游修复留出恢复窗口。
	require.False(t, unsupportedModelRecordEffective(record, now.Add(unsupportedModelBlacklistTTL+time.Hour), unsupportedModelBlacklistTTL))

	// 时间戳损坏时保守处理：继续拉黑，避免又踩确定性 404。
	broken := UnsupportedModelRecord{Count: 2, LastSeen: "not-a-timestamp"}
	require.True(t, unsupportedModelRecordEffective(broken, now, unsupportedModelBlacklistTTL))
}

func TestPruneExpiredUnsupportedModels(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-(unsupportedModelBlacklistTTL + time.Hour)).UTC().Format(time.RFC3339)
	fresh := now.UTC().Format(time.RFC3339)

	pruned := pruneExpiredUnsupportedModels(AccountUnsupportedModels{
		"stale-but-effective": {Count: 3, LastSeen: stale},
		"fresh":               {Count: 3, LastSeen: fresh},
		"still-observing":     {Count: 1, LastSeen: stale},
	}, now)

	require.NotContains(t, pruned, "stale-but-effective")
	require.Contains(t, pruned, "fresh")
	// 未达阈值的观察期记录要保留，否则永远攒不到阈值。
	require.Contains(t, pruned, "still-observing")
}

func TestUnsupportedModelRecordsDecodeFromExtraMap(t *testing.T) {
	// extra 从数据库读出时是 map[string]any（JSON 反序列化结果）。
	account := &Account{Extra: map[string]any{
		UnsupportedModelsExtraKey: map[string]any{
			"gpt-image-1": map[string]any{
				"status": float64(404), "count": float64(2),
				"last_seen": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}}
	require.True(t, account.IsModelMarkedUnsupported("gpt-image-1"))

	// 没有该键时走零分配快速路径。
	require.False(t, (&Account{}).IsModelMarkedUnsupported("gpt-image-1"))
	require.Nil(t, (&Account{}).UnsupportedModelRecords())
}

// 回归：拉黑必须在透传短路之前判定——透传账号同样会遇到"上游根本没有这个模型"，
// 那正是 per-(账号,模型) 404 冷却被反复触发的来源。
func TestIsModelSupportedExcludesUpstreamRejectedModel(t *testing.T) {
	account := unsupportedRecordAccount(AccountUnsupportedModels{
		"gpt-image-1": {Status: 404, Count: 2, LastSeen: time.Now().UTC().Format(time.RFC3339)},
	})
	require.False(t, account.IsModelSupported("gpt-image-1"))
	// 其它模型不受影响（无 model_mapping = 允许所有）。
	require.True(t, account.IsModelSupported("gpt-5.5"))
}

func TestUnsupportedModelExtraUpdate(t *testing.T) {
	records, _ := recordUnsupportedModel(nil, "gpt-image-1", 404, "upstream_404_model_not_found", time.Now())
	update := UnsupportedModelExtraUpdate(records)
	require.Contains(t, update, UnsupportedModelsExtraKey)

	// 空清单 → 显式 nil，让仓库层按既有约定删除该键。
	empty := UnsupportedModelExtraUpdate(nil)
	require.Nil(t, empty[UnsupportedModelsExtraKey])
}
