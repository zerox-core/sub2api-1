package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// unsupportedModelsAdminServiceStub 捕获写回的 extra 更新，用来断言放行后的清单内容。
type unsupportedModelsAdminServiceStub struct {
	*stubAdminService
	extraUpdates int
	lastUpdates  map[string]any
}

func (s *unsupportedModelsAdminServiceStub) UpdateAccountExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.extraUpdates++
	s.lastUpdates = updates
	return nil
}

func setupUnsupportedModelsRouter(account *service.Account) (*gin.Engine, *unsupportedModelsAdminServiceStub) {
	gin.SetMode(gin.TestMode)
	base := newStubAdminService()
	base.getAccountResult = account
	stub := &unsupportedModelsAdminServiceStub{stubAdminService: base}

	router := gin.New()
	handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.DELETE("/api/v1/admin/accounts/:id/unsupported-models", handler.ClearUnsupportedModels)
	return router, stub
}

func unsupportedModelsAccount() *service.Account {
	return &service.Account{
		ID:   5,
		Name: "zyuou",
		Extra: map[string]any{
			service.UnsupportedModelsExtraKey: map[string]any{
				"gpt-image-1": map[string]any{"status": 404, "count": 2, "reason": "upstream_404_model_not_found"},
				"gpt-5.4":     map[string]any{"status": 404, "count": 2, "reason": "upstream_404_model_not_found"},
			},
		},
	}
}

// 单条放行：模型名会先归一化（大小写、models/ 前缀），只删掉命中的那一条。
func TestClearUnsupportedModelsReleasesSingleModel(t *testing.T) {
	router, stub := setupUnsupportedModelsRouter(unsupportedModelsAccount())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/accounts/5/unsupported-models?model=models/GPT-Image-1", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, stub.extraUpdates)

	records, ok := stub.lastUpdates[service.UnsupportedModelsExtraKey].(service.AccountUnsupportedModels)
	require.True(t, ok, "放行后必须写回清单本身，而不是整包 nil")
	require.NotContains(t, records, "gpt-image-1")
	require.Contains(t, records, "gpt-5.4")

	// 响应里同步了进程内副本，客户端无需回查就能看到结果。
	var payload struct {
		Data struct {
			Extra map[string]any `json:"extra"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, records["gpt-5.4"].Count, 2)
	remaining, ok := payload.Data.Extra[service.UnsupportedModelsExtraKey].(map[string]any)
	require.True(t, ok, "extra.%s 应为对象", service.UnsupportedModelsExtraKey)
	require.NotContains(t, remaining, "gpt-image-1")
}

// 放行最后一条时写 nil，让仓储把 extra 里的键删掉，而不是留下空对象。
func TestClearUnsupportedModelsWritesNilWhenLastEntryReleased(t *testing.T) {
	account := unsupportedModelsAccount()
	account.Extra[service.UnsupportedModelsExtraKey] = map[string]any{
		"gpt-image-1": map[string]any{"status": 404, "count": 2},
	}
	router, stub := setupUnsupportedModelsRouter(account)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/accounts/5/unsupported-models?model=gpt-image-1", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, stub.lastUpdates[service.UnsupportedModelsExtraKey])
}

// 不带参数 = 全部放行。
func TestClearUnsupportedModelsReleasesAll(t *testing.T) {
	router, stub := setupUnsupportedModelsRouter(unsupportedModelsAccount())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/accounts/5/unsupported-models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, stub.extraUpdates)
	require.Nil(t, stub.lastUpdates[service.UnsupportedModelsExtraKey])
}

// 清单里没有的模型返回 404，并且不产生任何写入（避免误删整份清单）。
func TestClearUnsupportedModelsRejectsUnknownModel(t *testing.T) {
	router, stub := setupUnsupportedModelsRouter(unsupportedModelsAccount())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/accounts/5/unsupported-models?model=never-rejected", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, stub.extraUpdates)
}
