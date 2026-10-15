package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// unsupportedModelRepoStub 只实现 persistUnsupportedModel 用到的 UpdateExtra；
// 其余接口方法由内嵌的 AccountRepository 提供（不会被调用）。
type unsupportedModelRepoStub struct {
	AccountRepository
	updates map[string]map[string]any
}

func (r *unsupportedModelRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.updates == nil {
		r.updates = make(map[string]map[string]any)
	}
	key := fmt.Sprintf("%d", id)
	if r.updates[key] == nil {
		r.updates[key] = make(map[string]any)
	}
	for k, v := range updates {
		r.updates[key][k] = v
	}
	return nil
}

// 覆盖「上游明确 404 → 持久记账」这条写入路径：这是 R5 被动学习的入口，
// 单测走真实的 persistUnsupportedModel，而不是只测纯函数。
func TestPersistUnsupportedModelWritesAccountExtra(t *testing.T) {
	repo := &unsupportedModelRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	svc.persistUnsupportedModel(context.Background(), account, "gpt-image-1", 404, upstreamModelNotFoundReason)
	require.False(t, account.IsModelMarkedUnsupported("gpt-image-1"), "首次命中未达阈值")

	// 同一模型的另一种大小写写法应合并到同一条记录
	svc.persistUnsupportedModel(context.Background(), account, "GPT-Image-1", 404, upstreamModelNotFoundReason)
	require.True(t, account.IsModelMarkedUnsupported("gpt-image-1"))

	stored, ok := repo.updates["5"][UnsupportedModelsExtraKey]
	require.True(t, ok, "必须写回 accounts.extra")
	records, ok := stored.(AccountUnsupportedModels)
	require.True(t, ok)
	require.Equal(t, 2, records["gpt-image-1"].Count)
	require.Equal(t, 404, records["gpt-image-1"].Status)
	require.Equal(t, upstreamModelNotFoundReason, records["gpt-image-1"].Reason)
	require.NotEmpty(t, records["gpt-image-1"].FirstSeen)
	require.NotEmpty(t, records["gpt-image-1"].LastSeen)

	// 空模型名不记账
	svc.persistUnsupportedModel(context.Background(), account, "   ", 404, upstreamModelNotFoundReason)
	require.Len(t, account.UnsupportedModelRecords(), 1)
}

func TestPersistUnsupportedModelToleratesMissingRepo(t *testing.T) {
	// 服务未注入仓库时不得 panic（网关路径上容错优先）。
	svc := &RateLimitService{}
	account := &Account{ID: 5}
	require.NotPanics(t, func() {
		svc.persistUnsupportedModel(context.Background(), account, "gpt-image-1", 404, upstreamModelNotFoundReason)
	})
	require.Nil(t, account.UnsupportedModelRecords())
}
