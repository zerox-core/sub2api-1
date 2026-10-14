package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// AntigravityQuotaBucket 反重力额度桶（5h 窗口 / weekly 周额度）。
type AntigravityQuotaBucket struct {
	BucketID  string   `json:"bucket_id"`
	Window    string   `json:"window"` // "5h" | "weekly"
	ResetTime string   `json:"reset_time"`
	Remaining *float64 `json:"remaining"` // 0-1
}

// AntigravityQuotaGroup 模型额度组（Gemini Model / Claude and GPT）。
type AntigravityQuotaGroup struct {
	Key         string                   `json:"key"` // "gemini" | "third"
	DisplayName string                   `json:"display_name"`
	Buckets     []AntigravityQuotaBucket `json:"buckets"`
}

// AntigravityQuotaSnapshot 单账号额度摘要快照。
type AntigravityQuotaSnapshot struct {
	AccountID int64                   `json:"account_id"`
	Email     string                  `json:"email,omitempty"`
	FetchedAt string                  `json:"fetched_at"`
	Groups    []AntigravityQuotaGroup `json:"groups"`
}

const antigravityQuotaCacheTTL = 30 * time.Minute

// AntigravityQuotaService 拉取反重力账号的配额摘要（retrieveUserQuotaSummary）。
// 该端点按 User-Agent 做产品 license 校验（必须携带 antigravity UA，否则 403 #3501），
// 且不消耗对话额度；同一额度组内所有模型共享 5h + weekly 两个桶。
type AntigravityQuotaService struct {
	accountRepo AccountRepository

	mu        sync.Mutex
	cache     map[int64]antigravityQuotaCacheEntry
	transport http.RoundTripper
}

type antigravityQuotaCacheEntry struct {
	snapshot  *AntigravityQuotaSnapshot
	fetchedAt time.Time
}

// NewAntigravityQuotaService 创建反重力配额服务。
func NewAntigravityQuotaService(accountRepo AccountRepository) *AntigravityQuotaService {
	return &AntigravityQuotaService{
		accountRepo: accountRepo,
		cache:       make(map[int64]antigravityQuotaCacheEntry),
		transport:   http.DefaultTransport,
	}
}

// antigravityQuotaResp 上游 retrieveUserQuotaSummary 响应形状。
type antigravityQuotaResp struct {
	Groups []struct {
		DisplayName string `json:"displayName"`
		Buckets     []struct {
			BucketID          string   `json:"bucketId"`
			Window            string   `json:"window"`
			ResetTime         string   `json:"resetTime"`
			RemainingFraction *float64 `json:"remainingFraction"`
		} `json:"buckets"`
	} `json:"groups"`
}

// GetQuota 获取账号配额摘要（默认 30 分钟内存缓存，force=true 强制现查）。
func (s *AntigravityQuotaService) GetQuota(ctx context.Context, accountID int64, force bool) (*AntigravityQuotaSnapshot, error) {
	if !force {
		s.mu.Lock()
		if e, ok := s.cache[accountID]; ok && time.Since(e.fetchedAt) < antigravityQuotaCacheTTL {
			s.mu.Unlock()
			return e.snapshot, nil
		}
		s.mu.Unlock()
	}

	// GetByIDs 会预载 Proxy 关联（GetByID 不会）
	accounts, err := s.accountRepo.GetByIDs(ctx, []int64{accountID})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrAccountNotFound
	}
	account := accounts[0]
	if !account.IsAntigravityOAuth() {
		return nil, fmt.Errorf("账号 %s 不是 antigravity OAuth 账号", account.Name)
	}

	token, _ := account.Credentials["access_token"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("账号 %s 缺少 access_token，请重新完成 OAuth 授权", account.Name)
	}

	snapshot, err := s.fetchQuota(ctx, account, token)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[accountID] = antigravityQuotaCacheEntry{snapshot: snapshot, fetchedAt: time.Now()}
	s.mu.Unlock()
	return snapshot, nil
}

func (s *AntigravityQuotaService) fetchQuota(ctx context.Context, account *Account, token string) (*AntigravityQuotaSnapshot, error) {
	transport := s.transport
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL, err := url.Parse(account.Proxy.URL())
		if err != nil {
			return nil, fmt.Errorf("解析代理地址失败: %w", err)
		}
		transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}

	apiURL := antigravity.BaseURL + "/v1internal:retrieveUserQuotaSummary"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	// 仅这 3 个 header，与数据面请求保持一致；UA 是 license 校验的关键
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", antigravity.GetUserAgentForContext(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求配额接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取配额响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("配额查询返回 401，access_token 已过期且尚未刷新，请稍后重试: %s", msg)
		}
		return nil, fmt.Errorf("配额查询返回 HTTP %d: %s", resp.StatusCode, msg)
	}

	var parsed antigravityQuotaResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析配额响应失败: %w", err)
	}

	snapshot := &AntigravityQuotaSnapshot{
		AccountID: account.ID,
		FetchedAt: time.Now().Format("2006-01-02 15:04:05"),
		Groups:    make([]AntigravityQuotaGroup, 0, len(parsed.Groups)),
	}
	if email, ok := account.Credentials["email"].(string); ok {
		snapshot.Email = strings.TrimSpace(email)
	}
	for _, g := range parsed.Groups {
		group := AntigravityQuotaGroup{
			Key:         antigravityQuotaGroupKey(g.DisplayName),
			DisplayName: g.DisplayName,
			Buckets:     make([]AntigravityQuotaBucket, 0, len(g.Buckets)),
		}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, AntigravityQuotaBucket{
				BucketID:  b.BucketID,
				Window:    b.Window,
				ResetTime: b.ResetTime,
				Remaining: b.RemainingFraction,
			})
		}
		snapshot.Groups = append(snapshot.Groups, group)
	}
	return snapshot, nil
}

// antigravityQuotaGroupKey 与 hub 口径一致：显示名或桶 ID 含 gemini → gemini 组，否则 third（Claude/GPT）。
func antigravityQuotaGroupKey(displayName string) string {
	dn := strings.ToLower(displayName)
	if strings.Contains(dn, "gemini") {
		return "gemini"
	}
	return "third"
}
