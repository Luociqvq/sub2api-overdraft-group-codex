//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexQuotaOverdraftReconnectUpstream struct {
	httpUpstreamRecorder
	resetCalls     int
	resetProxyURL  string
	resetAccountID int64
}

func (u *codexQuotaOverdraftReconnectUpstream) ResetConnections(proxyURL string, accountID int64) {
	u.resetCalls++
	u.resetProxyURL = proxyURL
	u.resetAccountID = accountID
}

func TestCodexQuotaOverdraftInjection(t *testing.T) {
	t.Cleanup(func() { SetCodexQuotaOverdraftEnabled(false) })
	SetCodexQuotaOverdraftEnabled(true)
	ctx := WithCodexQuotaOverdraftScheduling(context.Background())
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{CodexQuotaOverdraftEnabled: true}}}
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)

	updated := svc.prepareCodexQuotaOverdraftBody(ctx, oauth, false, body)
	require.NotEqual(t, string(body), string(updated))

	var document codexQuotaOverdraftDocument
	require.NoError(t, json.Unmarshal(updated, &document))
	require.Len(t, document.Input, 3)
	var call, output codexQuotaOverdraftInputItem
	require.NoError(t, json.Unmarshal(document.Input[1], &call))
	require.NoError(t, json.Unmarshal(document.Input[2], &output))
	require.Equal(t, "custom_tool_call", call.Type)
	require.Equal(t, "custom_tool_call_output", output.Type)
	require.True(t, strings.HasPrefix(call.CallID, codexQuotaOverdraftCallIDPrefix))
	require.Equal(t, call.CallID, output.CallID)

	again := svc.prepareCodexQuotaOverdraftBody(ctx, oauth, false, updated)
	require.Equal(t, string(updated), string(again), "重复处理不能再次注入")
}

func TestCodexQuotaOverdraftHeaderless429ReconnectsOnFreshTransport(t *testing.T) {
	t.Cleanup(func() { SetCodexQuotaOverdraftEnabled(false) })
	SetCodexQuotaOverdraftEnabled(true)
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream := &codexQuotaOverdraftReconnectUpstream{httpUpstreamRecorder: httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"req-429"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error","message":"try again"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"req-ok"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_reconnected","status":"completed","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Gateway: config.GatewayConfig{CodexQuotaOverdraftEnabled: true}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 91, Name: "reconnect", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
		Status:      StatusActive, Schedulable: true,
	}

	result, err := svc.Forward(WithCodexQuotaOverdraftScheduling(context.Background()), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 2)
	require.Len(t, upstream.bodies, 2)
	require.True(t, upstream.requests[1].Close, "重试请求必须禁止复用上一条连接")
	require.Equal(t, 1, upstream.resetCalls)
	require.Equal(t, int64(91), upstream.resetAccountID)
	require.Contains(t, string(upstream.bodies[0]), codexQuotaOverdraftCallIDPrefix)
	require.Contains(t, string(upstream.bodies[1]), codexQuotaOverdraftCallIDPrefix)
}

func TestCodexQuotaOverdraft429ReconnectGuards(t *testing.T) {
	fiveHour := make(http.Header)
	fiveHour.Set("x-codex-primary-used-percent", "20")
	fiveHour.Set("x-codex-primary-window-minutes", "10080")
	fiveHour.Set("x-codex-secondary-used-percent", "100")
	fiveHour.Set("x-codex-secondary-window-minutes", "300")
	sevenDay := make(http.Header)
	sevenDay.Set("x-codex-primary-used-percent", "100")
	sevenDay.Set("x-codex-primary-window-minutes", "10080")
	sevenDay.Set("x-codex-secondary-used-percent", "20")
	sevenDay.Set("x-codex-secondary-window-minutes", "300")
	require.True(t, codexQuotaOverdraft429Reconnectable(nil, []byte(`{"error":{"message":"try again"}}`)))
	require.True(t, codexQuotaOverdraft429Reconnectable(fiveHour, nil))
	require.False(t, codexQuotaOverdraft429Reconnectable(sevenDay, nil))
	require.False(t, codexQuotaOverdraft429Reconnectable(nil, []byte(`{"error":{"message":"weekly limit reached"}}`)))
	require.False(t, codexQuotaOverdraft429Reconnectable(http.Header{"Retry-After": []string{"30"}}, nil))
}

func TestCodexQuotaOverdraftInjectionGuards(t *testing.T) {
	t.Cleanup(func() { SetCodexQuotaOverdraftEnabled(false) })
	body := []byte(`{"input":[{"type":"message","role":"user"}]}`)
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	enabledCfg := &config.Config{Gateway: config.GatewayConfig{CodexQuotaOverdraftEnabled: true}}
	svc := &OpenAIGatewayService{cfg: enabledCfg}

	SetCodexQuotaOverdraftEnabled(false)
	require.Equal(t, string(body), string(svc.prepareCodexQuotaOverdraftBody(WithCodexQuotaOverdraftScheduling(context.Background()), oauth, false, body)))

	SetCodexQuotaOverdraftEnabled(true)
	require.Equal(t, string(body), string(svc.prepareCodexQuotaOverdraftBody(context.Background(), oauth, false, body)), "未标记的端点不能注入")
	require.Equal(t, string(body), string(svc.prepareCodexQuotaOverdraftBody(WithCodexQuotaOverdraftScheduling(context.Background()), oauth, true, body)), "compact 不能注入")

	ctx := WithCodexQuotaOverdraftScheduling(context.Background())
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: int64PtrForCodexQuotaOverdraftTest(1)},
	} {
		require.Equal(t, string(body), string(svc.prepareCodexQuotaOverdraftBody(ctx, account, false, body)))
	}
	agentIdentity := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{openAIAuthModeCredentialKey: OpenAIAuthModeAgentIdentity}}
	require.NotEqual(t, string(body), string(svc.prepareCodexQuotaOverdraftBody(ctx, agentIdentity, false, body)), "Agent Identity 必须支持透支请求注入")

	notUserLast := []byte(`{"input":[{"type":"message","role":"assistant"}]}`)
	require.Equal(t, string(notUserLast), string(svc.prepareCodexQuotaOverdraftBody(ctx, oauth, false, notUserLast)))
	invalid := []byte(`{"input":`)
	require.Equal(t, string(invalid), string(svc.prepareCodexQuotaOverdraftBody(ctx, oauth, false, invalid)))
	oversized := make([]byte, codexQuotaOverdraftMaxBodyBytes+1)
	require.Equal(t, oversized, svc.prepareCodexQuotaOverdraftBody(ctx, oauth, false, oversized))
}

func TestCodexQuotaOverdraftSchedulingOnlyBypassesQuotaThresholds(t *testing.T) {
	t.Cleanup(func() { SetCodexQuotaOverdraftEnabled(false) })
	SetCodexQuotaOverdraftEnabled(true)
	ctx := WithCodexQuotaOverdraftScheduling(context.Background())
	now := time.Now().UTC()
	reset := now.Add(time.Hour)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_used_percent": 100,
			"codex_5h_reset_at":     reset.Format(time.RFC3339),
		},
	}
	quotaCtx := withOpenAIQuotaAutoPauseSettings(ctx, OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold5h: 0.8})
	paused, _ := shouldAutoPauseOpenAIAccountByQuota(quotaCtx, account)
	require.False(t, paused)

	account.RateLimitResetAt = &reset
	require.False(t, account.IsSchedulableForModelWithContext(quotaCtx, "gpt-5.4"), "真实 429 限流仍必须生效")
	account.RateLimitResetAt = nil
	account.TempUnschedulableUntil = &reset
	account.TempUnschedulableReason = BuildTempUnschedReasonPayload("oauth_401", "unauthorized")
	require.Same(t, account, normalizeCodexQuotaOverdraftAccountForScheduling(quotaCtx, account), "其他临时暂停不能绕过")

	account.TempUnschedulableReason = BuildAccountSchedulingThresholdReason("")
	normalized := normalizeCodexQuotaOverdraftAccountForScheduling(quotaCtx, account)
	require.NotSame(t, account, normalized)
	require.Nil(t, normalized.TempUnschedulableUntil)
	require.Empty(t, normalized.TempUnschedulableReason)
	require.NotNil(t, account.TempUnschedulableUntil, "不能修改缓存或数据库账号原对象")
}

func TestRateLimitServiceCodexQuotaOverdraftDoesNotCreateRuntimeThresholdBlock(t *testing.T) {
	t.Cleanup(func() { SetCodexQuotaOverdraftEnabled(false) })
	SetCodexQuotaOverdraftEnabled(true)
	accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
	accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})

	settingsRepo := newMockSettingRepo()
	settingsRepo.data[SettingKeyAccountSchedulingThresholds] = `{"openai":80}`
	accountRepo := &rateLimitAccountRepoStub{}
	runtimeBlocker := &runtimeBlockRecorder{}
	rl := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)
	rl.SetSettingService(NewSettingService(settingsRepo, &config.Config{}))
	rl.SetAccountRuntimeBlocker(runtimeBlocker)
	reset := time.Now().UTC().Add(time.Hour)
	account := &Account{
		ID:          9001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_7d_used_percent": 100,
			"codex_7d_reset_at":     reset.Format(time.RFC3339),
		},
	}

	require.True(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.tempCalls, "普通端点仍应持久化阈值暂停")
	require.Empty(t, runtimeBlocker.reasons, "阈值暂停不能误写为所有请求共享的 runtime blocker")
}

func int64PtrForCodexQuotaOverdraftTest(value int64) *int64 {
	return &value
}
