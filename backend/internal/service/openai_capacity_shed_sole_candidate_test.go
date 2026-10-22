package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// groupAwareSnapshotCache 按 bucket.GroupID 返回该分组当前的可调度账号，
// 用来复现"账号跨多个分组、其中某个分组只剩它自己"的判定。
type groupAwareSnapshotCache struct {
	SchedulerCache
	byGroup map[int64][]*Account
}

func (c *groupAwareSnapshotCache) GetSnapshot(_ context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	accounts := c.byGroup[bucket.GroupID]
	out := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		cloned := *account
		out = append(out, &cloned)
	}
	return out, true, nil
}

func newCapacityShedServiceWithGroups(repo AccountRepository, byGroup map[int64][]*Account) *OpenAIGatewayService {
	svc := newCapacityShedService(repo)
	svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &groupAwareSnapshotCache{byGroup: byGroup}}
	return svc
}

// 生产里有只有单个可调度账号的分组（生图/语音/国模/兜底/WorkBuddy 直连等专用组）。
// 把唯一账号打过载冷却，等于让那个分组在两分钟内直接回 No available accounts，
// 而降载往往只是秒级抖动，所以这种情形必须跳过冷却。
func TestCapacityShedSkipsCooldownWhenSoleCandidateInGroup(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	account := &Account{ID: 6301, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{41}}
	svc := newCapacityShedServiceWithGroups(repo, map[int64][]*Account{
		41: {account},
	})

	payload := []byte(capacityShedPayload)
	for range 2 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, http.StatusServiceUnavailable, http.Header{}, payload, "gpt-6.1-sol"))
	}

	require.Zero(t, repo.overloadCalls, "唯一出口的账号不得被冷却成空池")
}

func TestCapacityShedCooldownsWhenGroupHasAnotherCandidate(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	account := &Account{ID: 6302, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{13}}
	sibling := &Account{ID: 6303, Platform: PlatformOpenAI, GroupIDs: []int64{13}}
	svc := newCapacityShedServiceWithGroups(repo, map[int64][]*Account{
		13: {account, sibling},
	})

	payload := []byte(capacityShedPayload)
	for range 2 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, http.StatusServiceUnavailable, http.Header{}, payload, "gpt-6.1-sol"))
	}

	require.Equal(t, 1, repo.overloadCalls, "组内还有别的候选时必须把降载账号移出轮换")
	require.Equal(t, int64(6302), repo.lastOverloadID)
	require.WithinDuration(t, time.Now().Add(openAICapacityShedCooldown), repo.lastOverloadEnd, 3*time.Second)
}

// 账号跨多个分组时，只要其中任何一个分组会因为冷却而变空，就跳过冷却。
func TestCapacityShedSkipsCooldownWhenAnyGroupWouldEmpty(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	account := &Account{ID: 6304, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{14, 24}}
	sibling := &Account{ID: 6306, Platform: PlatformOpenAI, GroupIDs: []int64{14}}
	svc := newCapacityShedServiceWithGroups(repo, map[int64][]*Account{
		14: {account, sibling},
		24: {account},
	})

	for range 2 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, http.StatusServiceUnavailable, http.Header{},
			[]byte(capacityShedPayload), "gpt-6.1-sol"))
	}

	require.Zero(t, repo.overloadCalls, "组 24 会变空，因此整次冷却都应跳过")
}

// 对照组：两个分组都有别的候选时才真的冷却，确认上面两个用例的断言来自新增守卫。
func TestCapacityShedCooldownsWhenEveryGroupHasCandidate(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	account := &Account{ID: 6307, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{14, 24}}
	svc := newCapacityShedServiceWithGroups(repo, map[int64][]*Account{
		14: {account, {ID: 6308, Platform: PlatformOpenAI}},
		24: {account, {ID: 6309, Platform: PlatformOpenAI}},
	})

	for range 2 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, http.StatusServiceUnavailable, http.Header{},
			[]byte(capacityShedPayload), "gpt-6.1-sol"))
	}

	require.Equal(t, 1, repo.overloadCalls)
}

// 没有调度快照（简单模式 / 未初始化）时保持原语义：照常冷却。
func TestCapacityShedCooldownFailsOpenWithoutSnapshot(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{ID: 6305, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{13}}

	require.True(t, svc.capacityShedCooldownKeepsPoolServable(context.Background(), account))

	for range 2 {
		svc.handleOpenAIAccountUpstreamError(context.Background(), account,
			http.StatusServiceUnavailable, http.Header{}, []byte(capacityShedPayload), "gpt-6.1-sol")
	}
	require.Equal(t, 1, repo.overloadCalls)
}
