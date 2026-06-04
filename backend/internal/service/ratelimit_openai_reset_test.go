package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type openAI429ResetAccountRepoStub struct {
	AccountRepository
	rateLimitCalls    int
	lastRateLimitID   int64
	lastRateLimitTime time.Time
}

func (r *openAI429ResetAccountRepoStub) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	return nil
}

func (r *openAI429ResetAccountRepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastRateLimitTime = resetAt
	return nil
}

func TestHandle429_OpenAIPrefersBodyResetsInSecondsOverCodexHeaders(t *testing.T) {
	repo := &openAI429ResetAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 125, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"usage_limit_reached","message":"limit reached","resets_in_seconds":17}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(125), repo.lastRateLimitID)
	require.True(t,
		!repo.lastRateLimitTime.Before(before.Add(16*time.Second)) &&
			!repo.lastRateLimitTime.After(after.Add(18*time.Second)),
		"resetAt %s should be derived from body resets_in_seconds instead of x-codex headers",
		repo.lastRateLimitTime,
	)
}
