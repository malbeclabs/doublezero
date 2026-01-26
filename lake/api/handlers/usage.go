package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// Global usage tracking
var (
	globalUsageMu       sync.Mutex
	globalAlertedDate   string           // Date string (YYYY-MM-DD) when alerts were last tracked
	globalAlertedLevels map[int]bool     // Which threshold percentages have been alerted today
)

// GetGlobalDailyLimit returns the configured global daily limit (0 = unlimited)
func GetGlobalDailyLimit() int {
	limitStr := os.Getenv("USAGE_GLOBAL_DAILY_LIMIT")
	if limitStr == "" {
		return 0 // unlimited
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		slog.Warn("Invalid USAGE_GLOBAL_DAILY_LIMIT, using unlimited", "value", limitStr, "error", err)
		return 0
	}
	return limit
}

// GetMinSOLThreshold returns the minimum SOL balance for premium tier (in lamports)
func GetMinSOLThreshold() int64 {
	thresholdStr := os.Getenv("MIN_SOL_THRESHOLD")
	if thresholdStr == "" {
		return 1_000_000_000 // default: 1 SOL
	}
	threshold, err := strconv.ParseFloat(thresholdStr, 64)
	if err != nil {
		slog.Warn("Invalid MIN_SOL_THRESHOLD, using default 1.0 SOL", "value", thresholdStr, "error", err)
		return 1_000_000_000
	}
	return int64(threshold * 1_000_000_000) // convert SOL to lamports
}

// GetWalletPremiumLimit returns the daily question limit for premium wallet users
func GetWalletPremiumLimit() int {
	limitStr := os.Getenv("WALLET_PREMIUM_LIMIT")
	if limitStr == "" {
		return 25 // default
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		slog.Warn("Invalid WALLET_PREMIUM_LIMIT, using default 25", "value", limitStr, "error", err)
		return 25
	}
	return limit
}

// IsKillSwitchEnabled returns true if the kill switch is active (blocks all users)
func IsKillSwitchEnabled() bool {
	val := os.Getenv("USAGE_KILL_SWITCH")
	return val == "1" || val == "true" || val == "on"
}

// ErrKillSwitch is returned when the kill switch is enabled
var ErrKillSwitch = fmt.Errorf("service temporarily unavailable")

// ErrGlobalLimitExceeded is returned when the global daily limit is exceeded
var ErrGlobalLimitExceeded = fmt.Errorf("service daily limit reached, please try again tomorrow")

// UsageRecord represents a single usage record for tracking
type UsageRecord struct {
	QuestionCount int
	InputTokens   int64
	OutputTokens  int64
}

// IsPremiumWalletUser checks if an account is a wallet user with SOL balance >= threshold
func IsPremiumWalletUser(account *Account) bool {
	if account == nil || account.AccountType != AccountTypeWallet {
		return false
	}
	if account.SolBalance == nil {
		return false
	}
	return *account.SolBalance >= GetMinSOLThreshold()
}

// CheckQuota checks if the user/IP has remaining quota
// Returns remaining questions (nil = unlimited), and any error
func CheckQuota(ctx context.Context, account *Account, ip string) (*int, error) {
	// Check kill switch first - blocks everyone
	if IsKillSwitchEnabled() {
		slog.Warn("Kill switch is enabled, blocking request", "ip", ip)
		return intPtr(0), ErrKillSwitch
	}

	var accountType *string
	var accountID *uuid.UUID
	isDomainUser := false
	isPremiumWallet := false
	if account != nil {
		accountType = &account.AccountType
		accountID = &account.ID
		isDomainUser = account.AccountType == AccountTypeDomain
		isPremiumWallet = IsPremiumWalletUser(account)
		slog.Info("CheckQuota: authenticated user", "account_id", accountID, "account_type", account.AccountType, "is_domain", isDomainUser, "is_premium_wallet", isPremiumWallet)
	} else {
		slog.Info("CheckQuota: anonymous user", "ip", ip)
	}

	// Check global limit (soft block - only affects non-domain users)
	globalLimit := GetGlobalDailyLimit()
	if globalLimit > 0 && !isDomainUser {
		globalUsage, err := GetGlobalUsageToday(ctx)
		if err != nil {
			slog.Error("Failed to check global usage", "error", err)
			// Don't block on error, just log
		} else if globalUsage >= globalLimit {
			slog.Warn("Global limit exceeded, blocking non-domain user",
				"global_usage", globalUsage,
				"global_limit", globalLimit,
				"account_type", accountType,
				"ip", ip,
			)
			return intPtr(0), ErrGlobalLimitExceeded
		}
	}

	// Get the limit for this account type
	var limit *int
	var err error

	// Premium wallet users get a special limit
	if isPremiumWallet {
		premiumLimit := GetWalletPremiumLimit()
		limit = &premiumLimit
		slog.Info("CheckQuota: premium wallet user", "limit", premiumLimit)
	} else {
		err = config.PgPool.QueryRow(ctx, `
			SELECT daily_question_limit FROM usage_limits
			WHERE account_type IS NOT DISTINCT FROM $1
		`, accountType).Scan(&limit)
		if err != nil {
			// If no limit found, default to anonymous limit
			slog.Warn("CheckQuota: no limit found for account type, using default", "accountType", accountType, "error", err)
			limit = intPtr(5)
		} else {
			if limit == nil {
				slog.Info("CheckQuota: unlimited quota for account type", "accountType", accountType)
			} else {
				slog.Info("CheckQuota: limit found", "accountType", accountType, "limit", *limit)
			}
		}
	}

	// If unlimited, return nil
	if limit == nil {
		return nil, nil
	}

	// Get current usage for today
	var questionCount int
	if accountID != nil {
		err = config.PgPool.QueryRow(ctx, `
			SELECT COALESCE(question_count, 0) FROM usage_daily
			WHERE account_id = $1 AND date = CURRENT_DATE
		`, accountID).Scan(&questionCount)
	} else {
		err = config.PgPool.QueryRow(ctx, `
			SELECT COALESCE(question_count, 0) FROM usage_daily
			WHERE account_id IS NULL AND ip_address = $1 AND date = CURRENT_DATE
		`, ip).Scan(&questionCount)
	}
	if err != nil {
		// No usage record yet means 0 questions used
		questionCount = 0
	}

	remaining := *limit - questionCount
	if remaining < 0 {
		remaining = 0
	}
	return &remaining, nil
}

// GetGlobalUsageToday returns the total questions asked today across all users
func GetGlobalUsageToday(ctx context.Context) (int, error) {
	var total int
	err := config.PgPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(question_count), 0) FROM usage_daily
		WHERE date = CURRENT_DATE
	`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get global usage: %w", err)
	}
	return total, nil
}

// checkAndAlertGlobalUsage checks if global usage has crossed thresholds and logs warnings
func checkAndAlertGlobalUsage(ctx context.Context) {
	limit := GetGlobalDailyLimit()
	if limit <= 0 {
		return // unlimited, no alerts needed
	}

	total, err := GetGlobalUsageToday(ctx)
	if err != nil {
		slog.Error("Failed to get global usage for alerting", "error", err)
		return
	}

	// Update metrics
	utilization := float64(total) / float64(limit)
	metrics.SetUsageGlobalLimit(limit)
	metrics.SetUsageGlobalUtilization(utilization)

	// Check thresholds and alert
	today := time.Now().UTC().Format("2006-01-02")
	thresholds := []int{50, 80, 100}

	globalUsageMu.Lock()
	defer globalUsageMu.Unlock()

	// Reset alerts if it's a new day
	if globalAlertedDate != today {
		globalAlertedDate = today
		globalAlertedLevels = make(map[int]bool)
	}

	percentage := (total * 100) / limit

	for _, threshold := range thresholds {
		if percentage >= threshold && !globalAlertedLevels[threshold] {
			globalAlertedLevels[threshold] = true
			slog.Warn("Global usage threshold crossed",
				"threshold_percent", threshold,
				"current_usage", total,
				"daily_limit", limit,
				"utilization", fmt.Sprintf("%.1f%%", utilization*100),
			)
		}
	}
}

// RecordUsage records usage for a question/workflow
func RecordUsage(ctx context.Context, account *Account, ip string, usage UsageRecord) error {
	// Determine account type for metrics
	accountType := "anonymous"
	if account != nil {
		accountType = account.AccountType
	}

	if account != nil {
		// Authenticated user - use account_id
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO usage_daily (account_id, date, question_count, input_tokens, output_tokens)
			VALUES ($1, CURRENT_DATE, $2, $3, $4)
			ON CONFLICT (account_id, date) DO UPDATE SET
				question_count = usage_daily.question_count + EXCLUDED.question_count,
				input_tokens = usage_daily.input_tokens + EXCLUDED.input_tokens,
				output_tokens = usage_daily.output_tokens + EXCLUDED.output_tokens,
				updated_at = NOW()
		`, account.ID, usage.QuestionCount, usage.InputTokens, usage.OutputTokens)
		if err != nil {
			return fmt.Errorf("failed to record usage for account: %w", err)
		}
	} else {
		// Anonymous user - use IP address
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO usage_daily (ip_address, date, question_count, input_tokens, output_tokens)
			VALUES ($1::inet, CURRENT_DATE, $2, $3, $4)
			ON CONFLICT (ip_address, date) WHERE account_id IS NULL DO UPDATE SET
				question_count = usage_daily.question_count + EXCLUDED.question_count,
				input_tokens = usage_daily.input_tokens + EXCLUDED.input_tokens,
				output_tokens = usage_daily.output_tokens + EXCLUDED.output_tokens,
				updated_at = NOW()
		`, ip, usage.QuestionCount, usage.InputTokens, usage.OutputTokens)
		if err != nil {
			return fmt.Errorf("failed to record usage for IP: %w", err)
		}
	}

	// Record metrics
	if usage.QuestionCount > 0 {
		for i := 0; i < usage.QuestionCount; i++ {
			metrics.RecordUsageQuestion(accountType)
		}
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		metrics.RecordUsageTokens(accountType, usage.InputTokens, usage.OutputTokens)
	}

	// Check global usage thresholds
	checkAndAlertGlobalUsage(ctx)

	return nil
}

// IncrementQuestionCount is a convenience function to increment just the question count
func IncrementQuestionCount(ctx context.Context, account *Account, ip string) error {
	return RecordUsage(ctx, account, ip, UsageRecord{QuestionCount: 1})
}

// GetUsageToday returns today's usage for an account or IP
func GetUsageToday(ctx context.Context, account *Account, ip string) (*UsageRecord, error) {
	var questionCount int
	var inputTokens, outputTokens int64

	if account != nil {
		err := config.PgPool.QueryRow(ctx, `
			SELECT COALESCE(question_count, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0)
			FROM usage_daily
			WHERE account_id = $1 AND date = CURRENT_DATE
		`, account.ID).Scan(&questionCount, &inputTokens, &outputTokens)
		if err != nil {
			// No record yet
			return &UsageRecord{}, nil
		}
	} else {
		err := config.PgPool.QueryRow(ctx, `
			SELECT COALESCE(question_count, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0)
			FROM usage_daily
			WHERE account_id IS NULL AND ip_address = $1 AND date = CURRENT_DATE
		`, ip).Scan(&questionCount, &inputTokens, &outputTokens)
		if err != nil {
			// No record yet
			return &UsageRecord{}, nil
		}
	}

	return &UsageRecord{
		QuestionCount: questionCount,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
	}, nil
}

// CleanupExpiredSessions removes expired auth sessions
func CleanupExpiredSessions(ctx context.Context) error {
	_, err := config.PgPool.Exec(ctx, `
		DELETE FROM auth_sessions WHERE expires_at < NOW()
	`)
	return err
}

// CleanupExpiredNonces removes expired auth nonces
func CleanupExpiredNonces(ctx context.Context) error {
	_, err := config.PgPool.Exec(ctx, `
		DELETE FROM auth_nonces WHERE expires_at < NOW()
	`)
	return err
}

// CleanupOldUsageData removes usage data older than 90 days
func CleanupOldUsageData(ctx context.Context) error {
	_, err := config.PgPool.Exec(ctx, `
		DELETE FROM usage_daily WHERE date < CURRENT_DATE - INTERVAL '90 days'
	`)
	return err
}

// RunCleanupTasks runs all cleanup tasks (call periodically)
func RunCleanupTasks(ctx context.Context) {
	if err := CleanupExpiredSessions(ctx); err != nil {
		slog.Error("Failed to cleanup expired sessions", "error", err)
	}
	if err := CleanupExpiredNonces(ctx); err != nil {
		slog.Error("Failed to cleanup expired nonces", "error", err)
	}
	if err := CleanupOldUsageData(ctx); err != nil {
		slog.Error("Failed to cleanup old usage data", "error", err)
	}
}

// StartCleanupWorker starts a background worker that periodically cleans up expired data
func StartCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				RunCleanupTasks(ctx)
			}
		}
	}()
}

// InitUsageMetrics initializes usage metrics on startup
func InitUsageMetrics(ctx context.Context) {
	limit := GetGlobalDailyLimit()
	metrics.SetUsageGlobalLimit(limit)

	// Set initial daily gauge from database
	total, err := GetGlobalUsageToday(ctx)
	if err != nil {
		slog.Error("Failed to get initial global usage", "error", err)
		return
	}

	// Set the gauge to current daily total
	metrics.UsageQuestionsDailyGauge.Set(float64(total))

	if limit > 0 {
		utilization := float64(total) / float64(limit)
		metrics.SetUsageGlobalUtilization(utilization)
		slog.Info("Usage metrics initialized",
			"global_daily_limit", limit,
			"current_usage", total,
			"utilization", fmt.Sprintf("%.1f%%", utilization*100),
		)
	} else {
		slog.Info("Usage metrics initialized", "global_daily_limit", "unlimited", "current_usage", total)
	}
}

// StartDailyResetWorker starts a worker that resets daily metrics at midnight UTC
func StartDailyResetWorker(ctx context.Context) {
	go func() {
		for {
			// Calculate time until next midnight UTC
			now := time.Now().UTC()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			sleepDuration := nextMidnight.Sub(now)

			select {
			case <-ctx.Done():
				return
			case <-time.After(sleepDuration):
				slog.Info("Resetting daily usage metrics")
				metrics.ResetDailyGauge()

				// Reset alerted levels for new day
				globalUsageMu.Lock()
				globalAlertedDate = ""
				globalAlertedLevels = nil
				globalUsageMu.Unlock()
			}
		}
	}()
}
