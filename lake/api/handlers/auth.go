package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// Account types
const (
	AccountTypeDomain = "domain"
	AccountTypeWallet = "wallet"
)

// Account represents a user account
type Account struct {
	ID            uuid.UUID  `json:"id"`
	AccountType   string     `json:"account_type"`
	WalletAddress *string    `json:"wallet_address,omitempty"`
	Email         *string    `json:"email,omitempty"`
	EmailDomain   *string    `json:"email_domain,omitempty"`
	GoogleID      *string    `json:"google_id,omitempty"`
	DisplayName   *string    `json:"display_name,omitempty"`
	IsActive      bool       `json:"is_active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
}

// AuthSession represents an authentication session
type AuthSession struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// QuotaInfo represents current quota information
type QuotaInfo struct {
	Remaining *int   `json:"remaining"` // nil = unlimited
	Limit     *int   `json:"limit"`     // nil = unlimited
	ResetsAt  string `json:"resets_at"` // ISO timestamp
}

// MeResponse is the response for GET /api/auth/me
type MeResponse struct {
	Account *Account   `json:"account"`
	Quota   *QuotaInfo `json:"quota"`
}

// GoogleAuthRequest is the request for Google OAuth
type GoogleAuthRequest struct {
	IDToken     string  `json:"id_token"`
	AnonymousID *string `json:"anonymous_id,omitempty"` // For session migration
}

// GoogleAuthResponse is the response for Google OAuth
type GoogleAuthResponse struct {
	Token   string   `json:"token"`
	Account *Account `json:"account"`
}

// WalletNonceResponse is the response for nonce request
type WalletNonceResponse struct {
	Nonce string `json:"nonce"`
}

// WalletAuthRequest is the request for wallet authentication
type WalletAuthRequest struct {
	PublicKey   string  `json:"public_key"`
	Signature   string  `json:"signature"`
	Message     string  `json:"message"`
	AnonymousID *string `json:"anonymous_id,omitempty"` // For session migration
}

// WalletAuthResponse is the response for wallet authentication
type WalletAuthResponse struct {
	Token   string   `json:"token"`
	Account *Account `json:"account"`
}

// Session token lifetime
const sessionTokenLifetime = 30 * 24 * time.Hour // 30 days

// generateSessionToken generates a cryptographically secure session token
func generateSessionToken() (string, string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", "", err
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	return token, tokenHash, nil
}

// hashToken creates a SHA256 hash of a token
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// generateNonce generates a cryptographically secure nonce
func generateNonce() (string, error) {
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonceBytes), nil
}

// getAllowedDomains returns the list of allowed email domains for domain auth
func getAllowedDomains() []string {
	domains := os.Getenv("AUTH_ALLOWED_DOMAINS")
	if domains == "" {
		return []string{"doublezero.xyz", "malbeclabs.com"}
	}
	return strings.Split(domains, ",")
}

// isDomainAllowed checks if an email domain is in the allowed list
func isDomainAllowed(domain string) bool {
	allowed := getAllowedDomains()
	for _, d := range allowed {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// extractEmailDomain extracts the domain from an email address
func extractEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

// createSession creates a new auth session for an account
func createSession(ctx context.Context, accountID uuid.UUID) (string, error) {
	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	expiresAt := time.Now().Add(sessionTokenLifetime)

	_, err = config.PgPool.Exec(ctx, `
		INSERT INTO auth_sessions (account_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, accountID, tokenHash, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return token, nil
}

// GetAccountByToken retrieves an account by session token
func GetAccountByToken(ctx context.Context, token string) (*Account, error) {
	tokenHash := hashToken(token)

	var account Account
	var walletAddress, email, emailDomain, googleID, displayName *string
	var lastLoginAt *time.Time

	err := config.PgPool.QueryRow(ctx, `
		SELECT a.id, a.account_type, a.wallet_address, a.email, a.email_domain,
		       a.google_id, a.display_name, a.is_active, a.created_at, a.updated_at, a.last_login_at
		FROM accounts a
		INNER JOIN auth_sessions s ON s.account_id = a.id
		WHERE s.token_hash = $1 AND s.expires_at > NOW() AND a.is_active = true
	`, tokenHash).Scan(
		&account.ID, &account.AccountType, &walletAddress, &email, &emailDomain,
		&googleID, &displayName, &account.IsActive, &account.CreatedAt, &account.UpdatedAt, &lastLoginAt,
	)
	if err != nil {
		return nil, err
	}

	account.WalletAddress = walletAddress
	account.Email = email
	account.EmailDomain = emailDomain
	account.GoogleID = googleID
	account.DisplayName = displayName
	account.LastLoginAt = lastLoginAt

	return &account, nil
}

// GetQuotaForAccount returns quota info for an account (or anonymous by IP)
func GetQuotaForAccount(ctx context.Context, account *Account, ip string) (*QuotaInfo, error) {
	var accountType *string
	var accountID *uuid.UUID
	if account != nil {
		accountType = &account.AccountType
		accountID = &account.ID
	}

	// Get the limit for this account type
	var limit *int
	err := config.PgPool.QueryRow(ctx, `
		SELECT daily_question_limit FROM usage_limits
		WHERE account_type IS NOT DISTINCT FROM $1
	`, accountType).Scan(&limit)
	if err != nil {
		// If no limit found, default to anonymous limit
		limit = intPtr(5)
	}

	// Get current usage
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
		// No usage record yet
		questionCount = 0
	}

	// Calculate remaining
	var remaining *int
	if limit != nil {
		rem := *limit - questionCount
		if rem < 0 {
			rem = 0
		}
		remaining = &rem
	}

	return &QuotaInfo{
		Remaining: remaining,
		Limit:     limit,
		ResetsAt:  nextMidnightUTC().Format(time.RFC3339),
	}, nil
}

// nextMidnightUTC returns the next midnight UTC
func nextMidnightUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}

func intPtr(i int) *int {
	return &i
}

// migrateAnonymousSessions transfers sessions from anonymous_id to account_id
func migrateAnonymousSessions(ctx context.Context, accountID uuid.UUID, anonymousID string) error {
	if anonymousID == "" {
		return nil
	}

	result, err := config.PgPool.Exec(ctx, `
		UPDATE sessions
		SET account_id = $1, anonymous_id = NULL
		WHERE anonymous_id = $2 AND account_id IS NULL
	`, accountID, anonymousID)
	if err != nil {
		return fmt.Errorf("failed to migrate sessions: %w", err)
	}

	if result.RowsAffected() > 0 {
		slog.Info("Migrated anonymous sessions to account", "accountID", accountID, "anonymousID", anonymousID, "count", result.RowsAffected())
	}

	return nil
}

// GetAuthMe handles GET /api/auth/me
func GetAuthMe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	account := GetAccountFromContext(ctx)
	ip := GetIPFromRequest(r)

	quota, err := GetQuotaForAccount(ctx, account, ip)
	if err != nil {
		slog.Error("Failed to get quota", "error", err)
		// Return response anyway, just without quota
		quota = nil
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeResponse{
		Account: account,
		Quota:   quota,
	})
}

// PostAuthLogout handles POST /api/auth/logout
func PostAuthLogout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Get token from header
	token := extractBearerToken(r)
	if token == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	tokenHash := hashToken(token)

	// Delete the session
	_, err := config.PgPool.Exec(ctx, `
		DELETE FROM auth_sessions WHERE token_hash = $1
	`, tokenHash)
	if err != nil {
		slog.Error("Failed to delete session", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// GetAuthNonce handles GET /api/auth/nonce - returns a nonce for wallet signing
func GetAuthNonce(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nonce, err := generateNonce()
	if err != nil {
		slog.Error("Failed to generate nonce", "error", err)
		http.Error(w, "Failed to generate nonce", http.StatusInternalServerError)
		return
	}

	// Clean up expired nonces
	_, _ = config.PgPool.Exec(ctx, `DELETE FROM auth_nonces WHERE expires_at < NOW()`)

	// Store the nonce
	_, err = config.PgPool.Exec(ctx, `
		INSERT INTO auth_nonces (nonce, expires_at)
		VALUES ($1, NOW() + INTERVAL '5 minutes')
	`, nonce)
	if err != nil {
		slog.Error("Failed to store nonce", "error", err)
		http.Error(w, "Failed to store nonce", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WalletNonceResponse{Nonce: nonce})
}

// PostAuthWallet handles POST /api/auth/wallet - verifies wallet signature and creates session
func PostAuthWallet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var req WalletAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PublicKey == "" || req.Signature == "" || req.Message == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Validate public key format (Solana base58, 32-44 chars)
	if len(req.PublicKey) < 32 || len(req.PublicKey) > 44 {
		http.Error(w, "Invalid public key format", http.StatusBadRequest)
		return
	}

	// Extract nonce from message and verify it exists and is not expired
	// Message format: "Sign this message to authenticate with Lake.\n\nNonce: <nonce>"
	if !strings.Contains(req.Message, "Nonce: ") {
		http.Error(w, "Invalid message format", http.StatusBadRequest)
		return
	}

	nonceParts := strings.Split(req.Message, "Nonce: ")
	if len(nonceParts) != 2 {
		http.Error(w, "Invalid message format", http.StatusBadRequest)
		return
	}
	nonce := strings.TrimSpace(nonceParts[1])

	// Verify and delete nonce (one-time use)
	var expiresAt time.Time
	err := config.PgPool.QueryRow(ctx, `
		DELETE FROM auth_nonces WHERE nonce = $1 AND expires_at > NOW()
		RETURNING expires_at
	`, nonce).Scan(&expiresAt)
	if err != nil {
		slog.Warn("Invalid or expired nonce", "nonce", nonce, "error", err)
		http.Error(w, "Invalid or expired nonce", http.StatusUnauthorized)
		return
	}

	// Verify the signature using ed25519
	// The frontend signs with the wallet, we need to verify
	valid, err := verifyEd25519Signature(req.PublicKey, req.Message, req.Signature)
	if err != nil || !valid {
		slog.Warn("Invalid signature", "publicKey", req.PublicKey, "error", err)
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Find or create account
	var account Account
	var displayName *string

	err = config.PgPool.QueryRow(ctx, `
		INSERT INTO accounts (account_type, wallet_address)
		VALUES ($1, $2)
		ON CONFLICT (wallet_address) DO UPDATE SET last_login_at = NOW()
		RETURNING id, account_type, wallet_address, display_name, is_active, created_at, updated_at, last_login_at
	`, AccountTypeWallet, req.PublicKey).Scan(
		&account.ID, &account.AccountType, &account.WalletAddress,
		&displayName, &account.IsActive, &account.CreatedAt, &account.UpdatedAt, &account.LastLoginAt,
	)
	if err != nil {
		slog.Error("Failed to create/update account", "error", err)
		http.Error(w, "Failed to create account", http.StatusInternalServerError)
		return
	}
	account.DisplayName = displayName

	// Migrate anonymous sessions to this account
	if req.AnonymousID != nil && *req.AnonymousID != "" {
		if err := migrateAnonymousSessions(ctx, account.ID, *req.AnonymousID); err != nil {
			slog.Error("Failed to migrate sessions", "error", err)
			// Don't fail auth, just log
		}
	}

	// Create session
	token, err := createSession(ctx, account.ID)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WalletAuthResponse{
		Token:   token,
		Account: &account,
	})
}

// PostAuthGoogle handles POST /api/auth/google - verifies Google ID token and creates session
func PostAuthGoogle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var req GoogleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.IDToken == "" {
		http.Error(w, "Missing id_token", http.StatusBadRequest)
		return
	}

	// Verify the Google ID token
	claims, err := verifyGoogleIDToken(ctx, req.IDToken)
	if err != nil {
		slog.Warn("Invalid Google ID token", "error", err)
		http.Error(w, "Invalid ID token", http.StatusUnauthorized)
		return
	}

	// Check if email domain is allowed
	emailDomain := extractEmailDomain(claims.Email)
	if !isDomainAllowed(emailDomain) {
		slog.Warn("Email domain not allowed", "email", claims.Email, "domain", emailDomain)
		http.Error(w, "Email domain not authorized", http.StatusForbidden)
		return
	}

	// Find or create account
	var account Account
	var walletAddress, displayName *string

	err = config.PgPool.QueryRow(ctx, `
		INSERT INTO accounts (account_type, email, email_domain, google_id, display_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (google_id) DO UPDATE SET
			last_login_at = NOW(),
			display_name = COALESCE(EXCLUDED.display_name, accounts.display_name)
		RETURNING id, account_type, wallet_address, email, email_domain, google_id, display_name, is_active, created_at, updated_at, last_login_at
	`, AccountTypeDomain, claims.Email, emailDomain, claims.Subject, claims.Name).Scan(
		&account.ID, &account.AccountType, &walletAddress, &account.Email, &account.EmailDomain,
		&account.GoogleID, &displayName, &account.IsActive, &account.CreatedAt, &account.UpdatedAt, &account.LastLoginAt,
	)
	if err != nil {
		slog.Error("Failed to create/update account", "error", err)
		http.Error(w, "Failed to create account", http.StatusInternalServerError)
		return
	}
	account.WalletAddress = walletAddress
	account.DisplayName = displayName

	// Migrate anonymous sessions to this account
	if req.AnonymousID != nil && *req.AnonymousID != "" {
		if err := migrateAnonymousSessions(ctx, account.ID, *req.AnonymousID); err != nil {
			slog.Error("Failed to migrate sessions", "error", err)
			// Don't fail auth, just log
		}
	}

	// Create session
	token, err := createSession(ctx, account.ID)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GoogleAuthResponse{
		Token:   token,
		Account: &account,
	})
}

// GetUsageQuota handles GET /api/usage/quota
func GetUsageQuota(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	account := GetAccountFromContext(ctx)
	ip := GetIPFromRequest(r)

	quota, err := GetQuotaForAccount(ctx, account, ip)
	if err != nil {
		slog.Error("Failed to get quota", "error", err)
		http.Error(w, "Failed to get quota", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(quota)
}

// extractBearerToken extracts the token from Authorization header
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
