package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type usageBillingRepository struct {
	db     *sql.DB
	cache  *redis.Client
	stopCh chan struct{}
	wg     sync.WaitGroup
}

const (
	usageBillingDedupTTL       = 30 * 24 * time.Hour
	usageBillingFlushInterval  = time.Second
	usageBillingFlushTimeout   = 5 * time.Second
	usageBillingDirtyScanCount = 200
)

var usageBillingApplyScript = redis.NewScript(`
local existing = redis.call('GET', KEYS[1])
if existing ~= false then
	if existing ~= ARGV[1] then
		return {-1}
	end
	return {0}
end

redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])

local new_balance = ''
local api_key_quota_exhausted = 0

local balance_cost = tonumber(ARGV[3])
if balance_cost > 0 then
	redis.call('INCRBYFLOAT', KEYS[2], -balance_cost)
	redis.call('SADD', KEYS[3], ARGV[4])
	if redis.call('EXISTS', KEYS[4]) > 0 then
		new_balance = redis.call('INCRBYFLOAT', KEYS[4], -balance_cost)
		redis.call('EXPIRE', KEYS[4], ARGV[5])
	end
end

local subscription_cost = tonumber(ARGV[6])
local subscription_id = tonumber(ARGV[7])
if subscription_cost > 0 and subscription_id > 0 then
	redis.call('INCRBYFLOAT', KEYS[5], subscription_cost)
	redis.call('SADD', KEYS[6], ARGV[7])
	if tonumber(ARGV[8]) > 0 and redis.call('EXISTS', KEYS[7]) > 0 then
		redis.call('HINCRBYFLOAT', KEYS[7], 'daily_usage', subscription_cost)
		redis.call('HINCRBYFLOAT', KEYS[7], 'weekly_usage', subscription_cost)
		redis.call('HINCRBYFLOAT', KEYS[7], 'monthly_usage', subscription_cost)
		redis.call('EXPIRE', KEYS[7], ARGV[5])
	end
end

local api_key_quota_cost = tonumber(ARGV[9])
if api_key_quota_cost > 0 then
	local quota_delta = tonumber(redis.call('INCRBYFLOAT', KEYS[8], api_key_quota_cost))
	redis.call('SADD', KEYS[9], ARGV[10])
	local quota_used = tonumber(ARGV[19]) + quota_delta
	local quota = tonumber(ARGV[20])
	if redis.call('EXISTS', KEYS[10]) > 0 then
		quota_used = tonumber(redis.call('HINCRBYFLOAT', KEYS[10], 'quota_used', api_key_quota_cost))
		local cached_quota = tonumber(redis.call('HGET', KEYS[10], 'quota') or '0')
		if cached_quota > 0 then
			quota = cached_quota
		end
	end
	if quota > 0 and quota_used >= quota and (quota_used - api_key_quota_cost) < quota then
		api_key_quota_exhausted = 1
	end
end

local api_key_rate_limit_cost = tonumber(ARGV[11])
if api_key_rate_limit_cost > 0 then
	redis.call('INCRBYFLOAT', KEYS[11], api_key_rate_limit_cost)
	redis.call('SADD', KEYS[12], ARGV[10])
	if redis.call('EXISTS', KEYS[13]) > 0 then
		local now = tonumber(ARGV[12])
		local win5h = tonumber(ARGV[13])
		local win1d = tonumber(ARGV[14])
		local win7d = tonumber(ARGV[15])
		local function update_window(usage_field, window_field, window_duration)
			local w = tonumber(redis.call('HGET', KEYS[13], window_field) or 0)
			if w == 0 or (now - w) >= window_duration then
				redis.call('HSET', KEYS[13], usage_field, tostring(api_key_rate_limit_cost))
				redis.call('HSET', KEYS[13], window_field, tostring(now))
			else
				redis.call('HINCRBYFLOAT', KEYS[13], usage_field, api_key_rate_limit_cost)
			end
		end
		update_window('usage_5h', 'window_5h', win5h)
		update_window('usage_1d', 'window_1d', win1d)
		update_window('usage_7d', 'window_7d', win7d)
		redis.call('EXPIRE', KEYS[13], ARGV[16])
	end
end

local account_quota_cost = tonumber(ARGV[17])
if account_quota_cost > 0 and tonumber(ARGV[18]) > 0 then
	redis.call('INCRBYFLOAT', KEYS[14], account_quota_cost)
	redis.call('SADD', KEYS[15], ARGV[18])
end

return {1, new_balance, api_key_quota_exhausted}
`)

func NewUsageBillingRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return NewUsageBillingRepositoryWithRedis(client, sqlDB, nil)
}

func NewUsageBillingRepositoryWithRedis(_ *dbent.Client, sqlDB *sql.DB, rdb *redis.Client) service.UsageBillingRepository {
	return newUsageBillingRepository(sqlDB, rdb, true)
}

func newUsageBillingRepository(sqlDB *sql.DB, rdb *redis.Client, startFlushLoop bool) *usageBillingRepository {
	r := &usageBillingRepository{db: sqlDB, cache: rdb, stopCh: make(chan struct{})}
	if startFlushLoop && r.cache != nil && r.db != nil {
		r.wg.Add(1)
		go r.flushLoop()
	}
	return r
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	if r.cache != nil {
		return r.applyRedis(ctx, cmd)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyRedis(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	subscriptionID := usageBillingValueOrZero(cmd.SubscriptionID)
	groupID := usageBillingValueOrZero(cmd.GroupID)
	accountQuotaID := int64(0)
	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		accountQuotaID = cmd.AccountID
	}
	now := time.Now().Unix()
	raw, err := usageBillingApplyScript.Run(ctx, r.cache, []string{
		usageBillingDedupKey(cmd.RequestID, cmd.APIKeyID),
		usageBillingDirtyBalanceKey(cmd.UserID),
		usageBillingDirtyBalanceSetKey(),
		billingBalanceKey(cmd.UserID),
		usageBillingDirtySubscriptionKey(subscriptionID),
		usageBillingDirtySubscriptionSetKey(),
		billingSubKey(cmd.UserID, groupID),
		usageBillingDirtyAPIKeyQuotaKey(cmd.APIKeyID),
		usageBillingDirtyAPIKeyQuotaSetKey(),
		usageBillingAPIKeyQuotaCacheKey(cmd.APIKeyID),
		usageBillingDirtyAPIKeyRateLimitKey(cmd.APIKeyID),
		usageBillingDirtyAPIKeyRateLimitSetKey(),
		billingRateLimitKey(cmd.APIKeyID),
		usageBillingDirtyAccountQuotaKey(accountQuotaID),
		usageBillingDirtyAccountQuotaSetKey(),
	},
		cmd.RequestFingerprint,
		int(usageBillingDedupTTL.Seconds()),
		cmd.BalanceCost,
		cmd.UserID,
		int(jitteredTTL().Seconds()),
		cmd.SubscriptionCost,
		subscriptionID,
		groupID,
		cmd.APIKeyQuotaCost,
		cmd.APIKeyID,
		cmd.APIKeyRateLimitCost,
		now,
		int(rateLimitWindow5h.Seconds()),
		int(rateLimitWindow1d.Seconds()),
		int(rateLimitWindow7d.Seconds()),
		int(rateLimitCacheTTL.Seconds()),
		cmd.AccountQuotaCost,
		accountQuotaID,
		cmd.APIKeyQuotaUsed,
		cmd.APIKeyQuota,
	).Result()
	if err != nil {
		return nil, err
	}
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("unexpected usage billing redis result: %T", raw)
	}
	status, _ := values[0].(int64)
	switch status {
	case -1:
		return nil, service.ErrUsageBillingRequestConflict
	case 0:
		return &service.UsageBillingApplyResult{Applied: false}, nil
	case 1:
	default:
		return nil, fmt.Errorf("unexpected usage billing redis status: %d", status)
	}

	result := &service.UsageBillingApplyResult{Applied: true, CacheUpdated: true}
	if len(values) > 1 {
		if balance, ok := parseRedisFloat(values[1]); ok {
			result.NewBalance = &balance
		}
	}
	if len(values) > 2 {
		if exhausted, ok := values[2].(int64); ok && exhausted > 0 {
			result.APIKeyQuotaExhausted = true
		}
	}
	return result, nil
}

func usageBillingValueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func parseRedisFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case nil:
		return 0, false
	case string:
		if strings.TrimSpace(value) == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	case []byte:
		if len(value) == 0 {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(string(value), 64)
		return parsed, err == nil
	case int64:
		return float64(value), true
	case float64:
		return value, true
	default:
		return 0, false
	}
}

func usageBillingDedupKey(requestID string, apiKeyID int64) string {
	return fmt.Sprintf("usage_billing:dedup:%d:%s", apiKeyID, requestID)
}

func usageBillingDirtyBalanceSetKey() string { return "usage_billing:dirty:balances" }
func usageBillingDirtyBalanceKey(userID int64) string {
	return fmt.Sprintf("usage_billing:dirty:balance:%d", userID)
}
func usageBillingDirtySubscriptionSetKey() string { return "usage_billing:dirty:subscriptions" }
func usageBillingDirtySubscriptionKey(subscriptionID int64) string {
	return fmt.Sprintf("usage_billing:dirty:subscription:%d", subscriptionID)
}
func usageBillingDirtyAPIKeyQuotaSetKey() string { return "usage_billing:dirty:api_key_quotas" }
func usageBillingDirtyAPIKeyQuotaKey(apiKeyID int64) string {
	return fmt.Sprintf("usage_billing:dirty:api_key_quota:%d", apiKeyID)
}
func usageBillingAPIKeyQuotaCacheKey(apiKeyID int64) string {
	return fmt.Sprintf("usage_billing:cache:api_key_quota:%d", apiKeyID)
}
func usageBillingDirtyAPIKeyRateLimitSetKey() string {
	return "usage_billing:dirty:api_key_rate_limits"
}
func usageBillingDirtyAPIKeyRateLimitKey(apiKeyID int64) string {
	return fmt.Sprintf("usage_billing:dirty:api_key_rate_limit:%d", apiKeyID)
}
func usageBillingDirtyAccountQuotaSetKey() string { return "usage_billing:dirty:account_quotas" }
func usageBillingDirtyAccountQuotaKey(accountID int64) string {
	return fmt.Sprintf("usage_billing:dirty:account_quota:%d", accountID)
}

func (r *usageBillingRepository) flushLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(usageBillingFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), usageBillingFlushTimeout)
			if err := r.FlushDirty(ctx); err != nil {
				logger.LegacyPrintf("repository.usage_billing", "dirty flush failed: %v", err)
			}
			cancel()
		}
	}
}

func (r *usageBillingRepository) FlushDirty(ctx context.Context) error {
	if r == nil || r.cache == nil || r.db == nil {
		return nil
	}
	var errs []error
	if err := r.flushBalanceDeltas(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := r.flushSubscriptionDeltas(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := r.flushAPIKeyQuotaDeltas(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := r.flushAPIKeyRateLimitDeltas(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := r.flushAccountQuotaDeltas(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (r *usageBillingRepository) flushBalanceDeltas(ctx context.Context) error {
	return r.flushFloatDeltas(ctx, usageBillingDirtyBalanceSetKey(), usageBillingDirtyBalanceKey, func(ctx context.Context, id int64, delta float64) error {
		_, err := r.db.ExecContext(ctx, "UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL", delta, id)
		return err
	})
}

func (r *usageBillingRepository) flushSubscriptionDeltas(ctx context.Context) error {
	return r.flushFloatDeltas(ctx, usageBillingDirtySubscriptionSetKey(), usageBillingDirtySubscriptionKey, func(ctx context.Context, id int64, delta float64) error {
		_, err := r.db.ExecContext(ctx, `
			UPDATE user_subscriptions
			SET daily_usage_usd = daily_usage_usd + $1,
				weekly_usage_usd = weekly_usage_usd + $1,
				monthly_usage_usd = monthly_usage_usd + $1,
				updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`, delta, id)
		return err
	})
}

func (r *usageBillingRepository) flushAPIKeyQuotaDeltas(ctx context.Context) error {
	return r.flushFloatDeltas(ctx, usageBillingDirtyAPIKeyQuotaSetKey(), usageBillingDirtyAPIKeyQuotaKey, func(ctx context.Context, id int64, delta float64) error {
		var key string
		var status string
		err := r.db.QueryRowContext(ctx, `
			UPDATE api_keys
			SET quota_used = quota_used + $1,
				status = CASE
					WHEN quota > 0
						AND status = $2
						AND quota_used < quota
						AND quota_used + $1 >= quota
					THEN $3
					ELSE status
				END,
				updated_at = NOW()
			WHERE id = $4 AND deleted_at IS NULL
			RETURNING key, status
		`, delta, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted, id).Scan(&key, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAPIKeyNotFound
		}
		if err != nil {
			return err
		}
		if status == service.StatusAPIKeyQuotaExhausted {
			r.invalidateAPIKeyAuthCache(ctx, key)
		}
		return nil
	})
}

func (r *usageBillingRepository) invalidateAPIKeyAuthCache(ctx context.Context, key string) {
	if r == nil || r.cache == nil || strings.TrimSpace(key) == "" {
		return
	}
	sum := sha256.Sum256([]byte(key))
	cacheKey := hex.EncodeToString(sum[:])
	if err := r.cache.Del(ctx, apiKeyAuthCacheKey(cacheKey)).Err(); err != nil {
		logger.LegacyPrintf("repository.usage_billing", "invalidate api key auth cache failed: %v", err)
	}
	if err := r.cache.Publish(ctx, authCacheInvalidateChannel, cacheKey).Err(); err != nil {
		logger.LegacyPrintf("repository.usage_billing", "publish api key auth cache invalidation failed: %v", err)
	}
}

func (r *usageBillingRepository) flushAPIKeyRateLimitDeltas(ctx context.Context) error {
	return r.flushFloatDeltas(ctx, usageBillingDirtyAPIKeyRateLimitSetKey(), usageBillingDirtyAPIKeyRateLimitKey, func(ctx context.Context, id int64, delta float64) error {
		_, err := r.db.ExecContext(ctx, `
			UPDATE api_keys SET
				usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
				usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
				usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
				window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
				window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
				window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
				updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`, delta, id)
		return err
	})
}

func (r *usageBillingRepository) flushAccountQuotaDeltas(ctx context.Context) error {
	return r.flushFloatDeltas(ctx, usageBillingDirtyAccountQuotaSetKey(), usageBillingDirtyAccountQuotaKey, func(ctx context.Context, id int64, delta float64) error {
		var state service.AccountQuotaState
		err := r.db.QueryRowContext(ctx,
			`UPDATE accounts SET extra = (
				COALESCE(extra, '{}'::jsonb)
				|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
				|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
					jsonb_build_object(
						'quota_daily_used',
						CASE WHEN `+dailyExpiredExpr+`
						THEN $1
						ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
						'quota_daily_start',
						CASE WHEN `+dailyExpiredExpr+`
						THEN `+nowUTC+`
						ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
					)
					|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
					   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
					   ELSE '{}'::jsonb END
				ELSE '{}'::jsonb END
				|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
					jsonb_build_object(
						'quota_weekly_used',
						CASE WHEN `+weeklyExpiredExpr+`
						THEN $1
						ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
						'quota_weekly_start',
						CASE WHEN `+weeklyExpiredExpr+`
						THEN `+nowUTC+`
						ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
					)
					|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
					   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
					   ELSE '{}'::jsonb END
				ELSE '{}'::jsonb END
			), updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
			RETURNING
				COALESCE((extra->>'quota_used')::numeric, 0),
				COALESCE((extra->>'quota_limit')::numeric, 0),
				COALESCE((extra->>'quota_daily_used')::numeric, 0),
				COALESCE((extra->>'quota_daily_limit')::numeric, 0),
				COALESCE((extra->>'quota_weekly_used')::numeric, 0),
				COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
			delta, id).Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAccountNotFound
		}
		if err != nil {
			return err
		}
		crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-delta) < state.TotalLimit
		crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-delta) < state.DailyLimit
		crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-delta) < state.WeeklyLimit
		if crossedTotal || crossedDaily || crossedWeekly {
			if err := enqueueSchedulerOutbox(ctx, r.db, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
				logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", id, err)
				return err
			}
		}
		return nil
	})
}

func (r *usageBillingRepository) flushFloatDeltas(ctx context.Context, setKey string, keyFn func(int64) string, apply func(context.Context, int64, float64) error) error {
	ids, err := r.cache.SPopN(ctx, setKey, usageBillingDirtyScanCount).Result()
	if err != nil || len(ids) == 0 {
		return err
	}
	var errs []error
	for _, raw := range ids {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			errs = append(errs, parseErr)
			continue
		}
		key := keyFn(id)
		delta, err := r.cache.GetDel(ctx, key).Float64()
		if errors.Is(err, redis.Nil) || delta == 0 {
			continue
		}
		if err != nil {
			_, _ = r.cache.SAdd(ctx, setKey, id).Result()
			errs = append(errs, err)
			continue
		}
		if err := apply(ctx, id, delta); err != nil {
			_, _ = r.cache.IncrByFloat(ctx, key, delta).Result()
			_, _ = r.cache.SAdd(ctx, setKey, id).Result()
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
