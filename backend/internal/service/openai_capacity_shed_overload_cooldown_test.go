package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// capacityShedCooldownRepo 记录 SetOverloaded 调用。自定义而非复用 unit tag
// 下的 overloadAccountRepoStub，使这些用例跑在默认测试套件里。
type capacityShedCooldownRepo struct {
	AccountRepository
	overloadCalls   int
	lastOverloadID  int64
	lastOverloadEnd time.Time
}

func (r *capacityShedCooldownRepo) SetOverloaded(_ context.Context, id int64, until time.Time) error {
	r.overloadCalls++
	r.lastOverloadID = id
	r.lastOverloadEnd = until
	return nil
}

// 上游容量降载（"Our servers are currently overloaded" / server_is_overloaded）
// 的真实报文形状：HTTP 200 之后流内推 response.failed。
const capacityShedPayload = `{"type":"response.failed","response":{"status":"failed",` +
	`"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`

func newCapacityShedService(repo AccountRepository) *OpenAIGatewayService {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	return svc
}

func (s *OpenAIGatewayService) shedCapacity(t *testing.T, account *Account) bool {
	t.Helper()
	return s.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusServiceUnavailable, http.Header{},
		[]byte(capacityShedPayload), "gpt-6.1-sol")
}

// 回归：容量降载以前在 handleOpenAIAccountUpstreamError 里直接早退、不写任何状态，
// 于是调度器每个新请求都会重新选中同一个过载上游（用户看到的"号池不会绕开报错上游"）。
// 收窄后的语义：单次降载不记任何状态，同一账号连续两次才冷却 2 分钟。
func TestCapacityShedFirstEventDoesNotCooldown(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{ID: 6201, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	require.False(t, svc.shedCapacity(t, account), "容量降载只冷却账号，不应禁用账号")
	require.Zero(t, repo.overloadCalls, "单次降载是请求级信号，不应改变调度")
}

func TestCapacityShedSecondConsecutiveEventCooldownsTwoMinutes(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{ID: 6202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	before := time.Now()

	require.False(t, svc.shedCapacity(t, account))
	require.False(t, svc.shedCapacity(t, account))

	require.Equal(t, 1, repo.overloadCalls, "连续两次降载才把账号移出轮换")
	require.Equal(t, int64(6202), repo.lastOverloadID)
	require.WithinDuration(t, before.Add(2*time.Minute), repo.lastOverloadEnd, 3*time.Second)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "冷却不是禁用")
}

// 被一次成功隔开的两次降载不算“连续”。
func TestCapacityShedStreakResetsOnSuccess(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{ID: 6203, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	require.False(t, svc.shedCapacity(t, account))
	svc.ReportOpenAIAccountScheduleResult(account, "gpt-6.1-sol", true, nil)
	require.False(t, svc.shedCapacity(t, account))

	require.Zero(t, repo.overloadCalls, "成功之后的降载必须重新从 1 开始计数")
}

// 池模式保留既有豁免：一份本地凭证背后是多条上游 key，降载不代表账号整体故障。
func TestCapacityShedPoolModeKeepsExemption(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{
		ID: 6204, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true},
	}

	for range 3 {
		require.False(t, svc.shedCapacity(t, account))
	}

	require.Zero(t, repo.overloadCalls, "池模式不应把降载升级为账号级冷却")
}

// 普通 5xx 不能被这条路径顺手升级成账号级冷却：仍走既有的模型级短暂断路器。
func TestPlainGatewayErrorKeepsModelScopedBreaker(t *testing.T) {
	repo := &capacityShedCooldownRepo{}
	svc := newCapacityShedService(repo)
	account := &Account{ID: 6205, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"message":"temporary upstream failure"}}`)

	for range 2 {
		svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, http.StatusBadGateway, http.Header{}, body, "gpt-5.5")
	}

	require.Zero(t, repo.overloadCalls, "普通 5xx 不应触发账号级过载冷却")
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"),
		"普通 5xx 仍应只冷却失败的那个模型")
}

// 连续判定有时间窗：超过窗口的第二次降载重新计数。
func TestCapacityShedStreakWindowExpires(t *testing.T) {
	state := newOpenAICapacityShedStreakState(0)
	now := time.Now()

	if _, trip := state.recordFailure(7, now); trip {
		t.Fatal("第一次降载不应触发")
	}
	if _, trip := state.recordFailure(7, now.Add(openAICapacityShedStreakWindow+time.Minute)); trip {
		t.Fatal("超出时间窗后不应算作连续")
	}
	if _, trip := state.recordFailure(7, now.Add(openAICapacityShedStreakWindow+2*time.Minute)); !trip {
		t.Fatal("窗口内的第二次降载应触发")
	}
}
