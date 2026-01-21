package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// Session represents a chat or query session
type Session struct {
	ID          uuid.UUID       `json:"id"`
	Type        string          `json:"type"`
	Name        *string         `json:"name"`
	Content     json.RawMessage `json:"content"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	AccountID   *uuid.UUID      `json:"account_id,omitempty"`
	AnonymousID *string         `json:"anonymous_id,omitempty"`
}

// SessionListItem represents a session in list responses (without full content)
type SessionListItem struct {
	ID            uuid.UUID  `json:"id"`
	Type          string     `json:"type"`
	Name          *string    `json:"name"`
	ContentLength int        `json:"content_length"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	AccountID     *uuid.UUID `json:"account_id,omitempty"`
	AnonymousID   *string    `json:"anonymous_id,omitempty"`
}

// SessionListResponse is the response for listing sessions
type SessionListResponse struct {
	Sessions []SessionListItem `json:"sessions"`
	Total    int               `json:"total"`
	HasMore  bool              `json:"has_more"`
}

// SessionListWithContentResponse is the response for listing sessions with full content
type SessionListWithContentResponse struct {
	Sessions []Session `json:"sessions"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"has_more"`
}

// CreateSessionRequest is the request body for creating a session
type CreateSessionRequest struct {
	ID      uuid.UUID       `json:"id"`
	Type    string          `json:"type"`
	Name    *string         `json:"name"`
	Content json.RawMessage `json:"content"`
}

// UpdateSessionRequest is the request body for updating a session
type UpdateSessionRequest struct {
	Name    *string         `json:"name"`
	Content json.RawMessage `json:"content"`
}

// BatchGetSessionsRequest is the request body for batch fetching sessions
type BatchGetSessionsRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

// BatchGetSessionsResponse is the response for batch fetching sessions
type BatchGetSessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

// ListSessions returns a paginated list of sessions for the current user
func ListSessions(w http.ResponseWriter, r *http.Request) {
	sessionType := r.URL.Query().Get("type")
	if sessionType != "chat" && sessionType != "query" {
		http.Error(w, "type query parameter must be 'chat' or 'query'", http.StatusBadRequest)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	includeContent := r.URL.Query().Get("include_content") == "true"

	ctx := r.Context()

	// Get owner info from context
	account := GetAccountFromContext(ctx)
	anonymousID := r.URL.Query().Get("anonymous_id")

	// Build owner filter
	var ownerFilter string
	var ownerArg interface{}
	if account != nil {
		ownerFilter = "account_id = $2"
		ownerArg = account.ID
	} else if anonymousID != "" {
		ownerFilter = "anonymous_id = $2"
		ownerArg = anonymousID
	} else {
		// No owner specified - return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SessionListResponse{Sessions: []SessionListItem{}, Total: 0, HasMore: false})
		return
	}

	// Get total count
	var total int
	err := config.PgPool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM sessions WHERE type = $1 AND %s
	`, ownerFilter), sessionType, ownerArg).Scan(&total)
	if err != nil {
		http.Error(w, internalError("Failed to count sessions", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// If include_content is true, return full sessions
	if includeContent {
		rows, err := config.PgPool.Query(ctx, fmt.Sprintf(`
			SELECT id, type, name, content, created_at, updated_at, account_id, anonymous_id
			FROM sessions
			WHERE type = $1 AND %s
			ORDER BY updated_at DESC, id ASC
			LIMIT $3 OFFSET $4
		`, ownerFilter), sessionType, ownerArg, limit, offset)
		if err != nil {
			http.Error(w, internalError("Failed to list sessions", err), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sessions := []Session{}
		for rows.Next() {
			var s Session
			if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.Content, &s.CreatedAt, &s.UpdatedAt, &s.AccountID, &s.AnonymousID); err != nil {
				http.Error(w, internalError("Failed to scan session", err), http.StatusInternalServerError)
				return
			}
			sessions = append(sessions, s)
		}

		if err := rows.Err(); err != nil {
			http.Error(w, internalError("Failed to iterate sessions", err), http.StatusInternalServerError)
			return
		}

		response := SessionListWithContentResponse{
			Sessions: sessions,
			Total:    total,
			HasMore:  offset+len(sessions) < total,
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get sessions without content
	rows, err := config.PgPool.Query(ctx, fmt.Sprintf(`
		SELECT id, type, name, jsonb_array_length(content) as content_length,
		       created_at, updated_at, account_id, anonymous_id
		FROM sessions
		WHERE type = $1 AND %s
		ORDER BY updated_at DESC, id ASC
		LIMIT $3 OFFSET $4
	`, ownerFilter), sessionType, ownerArg, limit, offset)
	if err != nil {
		http.Error(w, internalError("Failed to list sessions", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []SessionListItem{}
	for rows.Next() {
		var s SessionListItem
		if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.ContentLength, &s.CreatedAt, &s.UpdatedAt, &s.AccountID, &s.AnonymousID); err != nil {
			http.Error(w, internalError("Failed to scan session", err), http.StatusInternalServerError)
			return
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, internalError("Failed to iterate sessions", err), http.StatusInternalServerError)
		return
	}

	response := SessionListResponse{
		Sessions: sessions,
		Total:    total,
		HasMore:  offset+len(sessions) < total,
	}
	json.NewEncoder(w).Encode(response)
}

// BatchGetSessionsRequestWithOwner includes anonymous_id
type BatchGetSessionsRequestWithOwner struct {
	IDs         []uuid.UUID `json:"ids"`
	AnonymousID *string     `json:"anonymous_id,omitempty"`
}

// BatchGetSessions returns multiple sessions by their IDs (filtered by owner)
func BatchGetSessions(w http.ResponseWriter, r *http.Request) {
	var req BatchGetSessionsRequestWithOwner
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BatchGetSessionsResponse{Sessions: []Session{}})
		return
	}

	// Cap at 50 IDs to prevent abuse
	if len(req.IDs) > 50 {
		req.IDs = req.IDs[:50]
	}

	ctx := r.Context()

	// Get owner info
	account := GetAccountFromContext(ctx)

	var rows interface {
		Next() bool
		Scan(dest ...interface{}) error
		Err() error
		Close()
	}
	var err error

	if account != nil {
		rows, err = config.PgPool.Query(ctx, `
			SELECT id, type, name, content, created_at, updated_at, account_id, anonymous_id
			FROM sessions
			WHERE id = ANY($1) AND account_id = $2
			ORDER BY updated_at DESC, id ASC
		`, req.IDs, account.ID)
	} else if req.AnonymousID != nil && *req.AnonymousID != "" {
		rows, err = config.PgPool.Query(ctx, `
			SELECT id, type, name, content, created_at, updated_at, account_id, anonymous_id
			FROM sessions
			WHERE id = ANY($1) AND anonymous_id = $2
			ORDER BY updated_at DESC, id ASC
		`, req.IDs, *req.AnonymousID)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BatchGetSessionsResponse{Sessions: []Session{}})
		return
	}

	if err != nil {
		http.Error(w, internalError("Failed to fetch sessions", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.Content, &s.CreatedAt, &s.UpdatedAt, &s.AccountID, &s.AnonymousID); err != nil {
			http.Error(w, internalError("Failed to scan session", err), http.StatusInternalServerError)
			return
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, internalError("Failed to iterate sessions", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BatchGetSessionsResponse{Sessions: sessions})
}

// GetSession returns a single session by ID (must belong to current user)
func GetSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get owner info
	account := GetAccountFromContext(ctx)
	anonymousID := r.URL.Query().Get("anonymous_id")

	var session Session
	err = config.PgPool.QueryRow(ctx, `
		SELECT id, type, name, content, created_at, updated_at, account_id, anonymous_id
		FROM sessions WHERE id = $1
	`, id).Scan(&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt, &session.AccountID, &session.AnonymousID)
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, internalError("Failed to get session", err), http.StatusInternalServerError)
		return
	}

	// Check ownership
	if account != nil {
		if session.AccountID == nil || *session.AccountID != account.ID {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
	} else if anonymousID != "" {
		if session.AnonymousID == nil || *session.AnonymousID != anonymousID {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
	} else {
		// No owner context - deny access
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// CreateSessionRequest is the request body for creating a session
type CreateSessionRequestWithOwner struct {
	ID          uuid.UUID       `json:"id"`
	Type        string          `json:"type"`
	Name        *string         `json:"name"`
	Content     json.RawMessage `json:"content"`
	AnonymousID *string         `json:"anonymous_id,omitempty"`
}

// CreateSession creates a new session owned by the current user
func CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequestWithOwner
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == uuid.Nil {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if req.Type != "chat" && req.Type != "query" {
		http.Error(w, "type must be 'chat' or 'query'", http.StatusBadRequest)
		return
	}

	if req.Content == nil {
		req.Content = json.RawMessage("[]")
	}

	ctx := r.Context()

	// Get owner info
	account := GetAccountFromContext(ctx)
	var accountID *uuid.UUID
	var anonymousID *string

	if account != nil {
		accountID = &account.ID
	} else if req.AnonymousID != nil && *req.AnonymousID != "" {
		anonymousID = req.AnonymousID
	} else {
		http.Error(w, "Authentication or anonymous_id required", http.StatusUnauthorized)
		return
	}

	var session Session
	err := config.PgPool.QueryRow(ctx, `
		INSERT INTO sessions (id, type, name, content, account_id, anonymous_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, type, name, content, created_at, updated_at, account_id, anonymous_id
	`, req.ID, req.Type, req.Name, req.Content, accountID, anonymousID).Scan(
		&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt, &session.AccountID, &session.AnonymousID,
	)
	if err != nil {
		// Check for duplicate key error
		if err.Error() == `ERROR: duplicate key value violates unique constraint "sessions_pkey" (SQLSTATE 23505)` {
			http.Error(w, "Session already exists", http.StatusConflict)
			return
		}
		http.Error(w, internalError("Failed to create session", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(session)
}

// UpdateSessionRequestWithOwner includes anonymous_id for ownership
type UpdateSessionRequestWithOwner struct {
	Name        *string         `json:"name"`
	Content     json.RawMessage `json:"content"`
	AnonymousID *string         `json:"anonymous_id,omitempty"`
}

// UpdateSession updates an existing session (must belong to current user)
func UpdateSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	var req UpdateSessionRequestWithOwner
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == nil {
		req.Content = json.RawMessage("[]")
	}

	ctx := r.Context()

	// Get owner info
	account := GetAccountFromContext(ctx)

	var session Session

	if account != nil {
		// Authenticated user - update if owned by them OR if orphaned (claim it)
		err = config.PgPool.QueryRow(ctx, `
			UPDATE sessions
			SET name = $2, content = $3, account_id = $4, updated_at = NOW()
			WHERE id = $1 AND (account_id = $4 OR (account_id IS NULL AND anonymous_id IS NULL))
			RETURNING id, type, name, content, created_at, updated_at, account_id, anonymous_id
		`, id, req.Name, req.Content, account.ID).Scan(
			&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt, &session.AccountID, &session.AnonymousID,
		)
	} else if req.AnonymousID != nil && *req.AnonymousID != "" {
		// Anonymous user - update if owned by them OR if orphaned (claim it)
		err = config.PgPool.QueryRow(ctx, `
			UPDATE sessions
			SET name = $2, content = $3, anonymous_id = $4, updated_at = NOW()
			WHERE id = $1 AND (anonymous_id = $4 OR (account_id IS NULL AND anonymous_id IS NULL))
			RETURNING id, type, name, content, created_at, updated_at, account_id, anonymous_id
		`, id, req.Name, req.Content, *req.AnonymousID).Scan(
			&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt, &session.AccountID, &session.AnonymousID,
		)
	} else {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, internalError("Failed to update session", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// DeleteSession deletes a session by ID (must belong to current user)
func DeleteSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get owner info
	account := GetAccountFromContext(ctx)
	anonymousID := r.URL.Query().Get("anonymous_id")

	var result interface{ RowsAffected() int64 }
	if account != nil {
		result, err = config.PgPool.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND account_id = $2`, id, account.ID)
	} else if anonymousID != "" {
		result, err = config.PgPool.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND anonymous_id = $2`, id, anonymousID)
	} else {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	if err != nil {
		http.Error(w, internalError("Failed to delete session", err), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
