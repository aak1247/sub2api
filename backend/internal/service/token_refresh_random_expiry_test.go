package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type randomExpiryAccountRepoStub struct {
	AccountRepository

	account          *Account
	updateExtraCalls int
	updateCalls      int
	lastExtraUpdates map[string]any
}

func (r *randomExpiryAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	if r.account == nil {
		return nil, errors.New("account not found")
	}
	return r.account, nil
}

func (r *randomExpiryAccountRepoStub) ListActive(context.Context) ([]Account, error) {
	if r.account == nil {
		return nil, nil
	}
	return []Account{*r.account}, nil
}

func (r *randomExpiryAccountRepoStub) Update(context.Context, *Account) error {
	r.updateCalls++
	return nil
}

func (r *randomExpiryAccountRepoStub) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.updateCalls++
	if r.account == nil || r.account.ID != id {
		r.account = &Account{ID: id}
	}
	r.account.Credentials = cloneCredentials(credentials)
	return nil
}

func (r *randomExpiryAccountRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtraUpdates = updates
	if r.account != nil && r.account.ID == id {
		if r.account.Extra == nil {
			r.account.Extra = make(map[string]any)
		}
		for k, v := range updates {
			r.account.Extra[k] = v
		}
	}
	return nil
}

func (r *randomExpiryAccountRepoStub) ClearError(context.Context, int64) error { return nil }
func (r *randomExpiryAccountRepoStub) ClearTempUnschedulable(context.Context, int64) error {
	return nil
}

func (r *randomExpiryAccountRepoStub) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

type randomExpiryRefresherStub struct {
	credentials map[string]any
	calls       int
}

func (r *randomExpiryRefresherStub) CanRefresh(account *Account) bool {
	return account != nil && account.Type == AccountTypeOAuth
}

func (r *randomExpiryRefresherStub) NeedsRefresh(*Account, time.Duration) bool { return false }

func (r *randomExpiryRefresherStub) Refresh(context.Context, *Account) (map[string]any, error) {
	r.calls++
	return r.credentials, nil
}

func TestRandomExpiryRefreshAt_WithinHourBeforeExpiry(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(2 * time.Hour)

	for range 100 {
		got := randomExpiryRefreshAt(now, expiresAt)
		require.False(t, got.Before(expiresAt.Add(-time.Hour)))
		require.False(t, got.After(expiresAt.Add(-tokenRefreshSafetyRefreshWindow)))
		require.True(t, got.Before(expiresAt))
	}
}

func TestTokenRefreshService_NeedsRandomExpiryRefresh_SchedulesBeforeExpiry(t *testing.T) {
	expiresAt := time.Now().Add(2 * time.Hour).UTC()
	account := &Account{
		ID:   31,
		Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt.Format(time.RFC3339),
		},
	}
	repo := &randomExpiryAccountRepoStub{account: account}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{
		TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1},
	}, nil)

	require.False(t, svc.needsRandomExpiryRefresh(context.Background(), account))
	require.Equal(t, 1, repo.updateExtraCalls)

	nextRefreshAt, ok := tokenRefreshScheduledAt(account)
	require.True(t, ok)
	require.False(t, nextRefreshAt.Before(expiresAt.Add(-time.Hour)))
	require.False(t, nextRefreshAt.After(expiresAt.Add(-tokenRefreshSafetyRefreshWindow)))
}

func TestTokenRefreshService_NeedsRandomExpiryRefresh_Due(t *testing.T) {
	expiresAt := time.Now().Add(30 * time.Minute).UTC()
	account := &Account{
		ID:   32,
		Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt.Format(time.RFC3339),
		},
		Extra: map[string]any{
			tokenRefreshNextScheduledAtExtraKey: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		},
	}
	repo := &randomExpiryAccountRepoStub{account: account}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{
		TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1},
	}, nil)

	require.True(t, svc.needsRandomExpiryRefresh(context.Background(), account))
	require.Equal(t, 0, repo.updateExtraCalls)
}

func TestTokenRefreshService_ProcessRefresh_UsesRandomExpiryScheduleInsteadOfConfiguredInterval(t *testing.T) {
	oldExpiresAt := time.Now().Add(30 * time.Minute).UTC()
	newExpiresAt := time.Now().Add(2 * time.Hour).UTC()
	account := &Account{
		ID:       33,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at":   oldExpiresAt.Format(time.RFC3339),
			"access_token": "old-token",
		},
		Extra: map[string]any{
			tokenRefreshNextScheduledAtExtraKey: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		},
	}
	repo := &randomExpiryAccountRepoStub{account: account}
	refresher := &randomExpiryRefresherStub{credentials: map[string]any{
		"expires_at":   newExpiresAt.Format(time.RFC3339),
		"access_token": "new-token",
	}}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:                         1,
			ScheduledRefreshMinIntervalMinutes: 1,
			ScheduledRefreshMaxIntervalMinutes: 1,
		},
	}, nil)
	svc.refreshers = []TokenRefresher{refresher}

	svc.processRefresh()

	require.Equal(t, 1, refresher.calls)
	require.Equal(t, 1, repo.updateCalls)
	nextRefreshAt, ok := tokenRefreshScheduledAt(account)
	require.True(t, ok)
	require.False(t, nextRefreshAt.Before(newExpiresAt.Add(-time.Hour)))
	require.False(t, nextRefreshAt.After(newExpiresAt.Add(-tokenRefreshSafetyRefreshWindow)))
}
