package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// tierTestConfig 构造与生产一致的分批负载调度配置（load_batch_enabled 默认 true）。
func tierTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	return cfg
}

// tierTestAccount 构造一个可调度的 OpenAI APIKey 账号。
func tierTestAccount(id int64, tier int) *Account {
	account := &Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Priority:    tier,
	}
	if tier > 0 {
		account.Extra = map[string]any{OpenAISchedulerTierExtraKey: tier}
	}
	return account
}

func TestSelectByLoadBalanceTierPrefersPrimaryWhenAvailable(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	primary := tierTestAccount(4101, 0)
	backstop := tierTestAccount(4102, 1)
	groupID := int64(1)

	acquiredIDs := []int64{}
	cache := schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*primary, *backstop}},
		cfg:                tierTestConfig(),
		concurrencyService: NewConcurrencyService(cache),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-test", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, primary.ID, selection.Account.ID)
	require.NotContains(t, acquiredIDs, backstop.ID)
	selection.ReleaseFunc()
}

func TestSelectByLoadBalanceTierSpillsWhenPrimarySaturated(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	primary := tierTestAccount(4201, 0)
	backstop := tierTestAccount(4202, 1)
	groupID := int64(1)

	acquiredIDs := []int64{}
	cache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{primary.ID: false, backstop.ID: true},
		loadMap: map[int64]*AccountLoadInfo{
			primary.ID:  {AccountID: primary.ID, CurrentConcurrency: 2, LoadRate: 100},
			backstop.ID: {AccountID: backstop.ID},
		},
		skipDefaultLoad: true,
		acquiredIDs:     &acquiredIDs,
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*primary, *backstop}},
		cfg:                tierTestConfig(),
		concurrencyService: NewConcurrencyService(cache),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-test", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, backstop.ID, selection.Account.ID)
	// 主用层 LoadRate=100 已在负载预过滤阶段被排除，因此这里断言"没有选中主用层"
	// 而不是"尝试过主用层"。
	require.NotContains(t, acquiredIDs, primary.ID)
	selection.ReleaseFunc()
}

// TestSelectByLoadBalanceTierSpillsWhenPrimarySlotContended 覆盖更贴近生产的场景：
// 主用层负载未满（LoadRate < 100）但并发槽位已被占满，抢槽失败后必须下探托底层。
func TestSelectByLoadBalanceTierSpillsWhenPrimarySlotContended(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	primary := tierTestAccount(4501, 0)
	backstop := tierTestAccount(4502, 1)
	groupID := int64(1)

	acquiredIDs := []int64{}
	cache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{primary.ID: false, backstop.ID: true},
		loadMap: map[int64]*AccountLoadInfo{
			primary.ID:  {AccountID: primary.ID, CurrentConcurrency: 1, LoadRate: 50},
			backstop.ID: {AccountID: backstop.ID},
		},
		skipDefaultLoad: true,
		acquiredIDs:     &acquiredIDs,
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*primary, *backstop}},
		cfg:                tierTestConfig(),
		concurrencyService: NewConcurrencyService(cache),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-test", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, backstop.ID, selection.Account.ID, "主用层抢槽失败后必须下探托底层")
	require.Contains(t, acquiredIDs, primary.ID, "必须先尝试主用层抢槽")
	selection.ReleaseFunc()
}

func TestSelectByLoadBalanceTierExcludedPrimarySpills(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	primary := tierTestAccount(4301, 0)
	backstop := tierTestAccount(4302, 1)
	groupID := int64(1)

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*primary, *backstop}},
		cfg:                tierTestConfig(),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-test",
		map[int64]struct{}{primary.ID: {}}, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, backstop.ID, selection.Account.ID)
	selection.ReleaseFunc()
}

func TestSelectByLoadBalanceTierWithoutBackstopUnchanged(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	first := tierTestAccount(4401, 0)
	second := tierTestAccount(4402, 0)
	groupID := int64(1)

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*first, *second}},
		cfg:                tierTestConfig(),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-test", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Contains(t, []int64{first.ID, second.ID}, selection.Account.ID)
	selection.ReleaseFunc()
}
