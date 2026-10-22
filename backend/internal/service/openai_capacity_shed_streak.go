package service

import (
	"sync"
	"time"
)

const (
	// 容量降载是请求级信号：单次降载只说明这一次请求被上游丢进降载路径，
	// 不足以判定账号不可用；同一账号连续被降载才说明它选中的那条上游在持续
	// 降载，此时把账号短暂移出轮换，让后续请求换到别的号。
	openAICapacityShedStreakThreshold = 2
	// 连续判定的时间窗：超过这个间隔的两次降载不算“连续”。
	openAICapacityShedStreakWindow = 10 * time.Minute
	// 触发后的冷却时长。刻意取短：降载是上游瞬时状态，账号本身没坏。
	openAICapacityShedCooldown   = 2 * time.Minute
	openAICapacityShedMaxEntries = 4096
	// 容量降载的同账号重试上限：只做一次即时确认重试，随后立刻换账号/渠道。
	// 降到 1 的理由是首字延迟：默认预算 3 次在请求级瞬时错误下按 500ms/1s/2s 退避，
	// 等于先在被降载的上游上耗掉约 3.5 秒才轮到同组其他账号；而降载本身是上游容量
	// 问题，同一账号在几秒内几乎必然再次降载，只有换号（独立账号/渠道的容量互不相干）
	// 才有机会立刻拿到首字。语义不变：仍然是"先在同账号上有界重试，再切号"。
	openAICapacityShedSameAccountRetryMax = 1
)

type openAICapacityShedEntry struct {
	streak      int
	lastFailure time.Time
}

// openAICapacityShedStreakState 按账号累计“连续被上游容量降载”的次数。
// 任何一次成功请求都会清零（见 ReportOpenAIAccountScheduleResult）。
type openAICapacityShedStreakState struct {
	mu         sync.Mutex
	entries    map[int64]openAICapacityShedEntry
	maxEntries int
}

func newOpenAICapacityShedStreakState(maxEntries int) *openAICapacityShedStreakState {
	if maxEntries <= 0 {
		maxEntries = openAICapacityShedMaxEntries
	}
	return &openAICapacityShedStreakState{
		entries:    make(map[int64]openAICapacityShedEntry),
		maxEntries: maxEntries,
	}
}

// recordFailure 记一次降载，返回当前连续次数以及是否达到触发阈值。
func (s *openAICapacityShedStreakState) recordFailure(accountID int64, now time.Time) (int, bool) {
	if s == nil || accountID <= 0 {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[int64]openAICapacityShedEntry)
	}
	if s.maxEntries <= 0 {
		s.maxEntries = openAICapacityShedMaxEntries
	}

	entry, exists := s.entries[accountID]
	if !exists {
		s.evictOldestLocked()
	}
	if !exists || entry.lastFailure.IsZero() || now.Sub(entry.lastFailure) > openAICapacityShedStreakWindow || now.Before(entry.lastFailure) {
		entry.streak = 0
	}
	entry.streak++
	entry.lastFailure = now
	s.entries[accountID] = entry
	return entry.streak, entry.streak >= openAICapacityShedStreakThreshold
}

func (s *openAICapacityShedStreakState) recordSuccess(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.entries, accountID)
	s.mu.Unlock()
}

func (s *openAICapacityShedStreakState) evictOldestLocked() {
	if len(s.entries) < s.maxEntries {
		return
	}
	var oldestID int64
	var oldestTime time.Time
	found := false
	for id, entry := range s.entries {
		if !found || entry.lastFailure.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.lastFailure
			found = true
		}
	}
	if found {
		delete(s.entries, oldestID)
	}
}

func (s *OpenAIGatewayService) getOpenAICapacityShedStreakState() *openAICapacityShedStreakState {
	if s == nil {
		return nil
	}
	s.openaiCapacityShedOnce.Do(func() {
		if s.openaiCapacityShedStreak == nil {
			s.openaiCapacityShedStreak = newOpenAICapacityShedStreakState(openAICapacityShedMaxEntries)
		}
	})
	return s.openaiCapacityShedStreak
}

// recordOpenAICapacityShedFailure 记一次容量降载，返回连续次数与是否达到触发阈值。
func (s *OpenAIGatewayService) recordOpenAICapacityShedFailure(account *Account) (int, bool) {
	if s == nil || account == nil || account.ID <= 0 {
		return 0, false
	}
	state := s.getOpenAICapacityShedStreakState()
	if state == nil {
		return 0, false
	}
	return state.recordFailure(account.ID, time.Now())
}

// clearOpenAICapacityShedStreak 在账号成功产出后清零，避免把两次被成功隔开的
// 降载误判成“连续降载”。
func (s *OpenAIGatewayService) clearOpenAICapacityShedStreak(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	state := s.getOpenAICapacityShedStreakState()
	if state == nil {
		return
	}
	state.recordSuccess(accountID)
}
