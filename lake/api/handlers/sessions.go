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
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Name      *string         `json:"name"`
	Content   json.RawMessage `json:"content"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// SessionListItem represents a session in list responses (without full content)
type SessionListItem struct {
	ID            uuid.UUID `json:"id"`
	Type          string    `json:"type"`
	Name          *string   `json:"name"`
	ContentLength int       `json:"content_length"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

// ListSessions returns a paginated list of sessions
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

	// Get total count
	var total int
	err := config.PgPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sessions WHERE type = $1
	`, sessionType).Scan(&total)
	if err != nil {
		http.Error(w, internalError("Failed to count sessions", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// If include_content is true, return full sessions
	if includeContent {
		rows, err := config.PgPool.Query(ctx, `
			SELECT id, type, name, content, created_at, updated_at
			FROM sessions
			WHERE type = $1
			ORDER BY updated_at DESC, id ASC
			LIMIT $2 OFFSET $3
		`, sessionType, limit, offset)
		if err != nil {
			http.Error(w, internalError("Failed to list sessions", err), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sessions := []Session{}
		for rows.Next() {
			var s Session
			if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.Content, &s.CreatedAt, &s.UpdatedAt); err != nil {
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
	// Use id as secondary sort key for stable ordering when timestamps are equal
	rows, err := config.PgPool.Query(ctx, `
		SELECT id, type, name, jsonb_array_length(content) as content_length,
		       created_at, updated_at
		FROM sessions
		WHERE type = $1
		ORDER BY updated_at DESC, id ASC
		LIMIT $2 OFFSET $3
	`, sessionType, limit, offset)
	if err != nil {
		http.Error(w, internalError("Failed to list sessions", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []SessionListItem{}
	for rows.Next() {
		var s SessionListItem
		if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.ContentLength, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

// BatchGetSessions returns multiple sessions by their IDs
func BatchGetSessions(w http.ResponseWriter, r *http.Request) {
	var req BatchGetSessionsRequest
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

	rows, err := config.PgPool.Query(ctx, `
		SELECT id, type, name, content, created_at, updated_at
		FROM sessions
		WHERE id = ANY($1)
		ORDER BY updated_at DESC, id ASC
	`, req.IDs)
	if err != nil {
		http.Error(w, internalError("Failed to fetch sessions", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.Content, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

// GetSession returns a single session by ID
func GetSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var session Session
	err = config.PgPool.QueryRow(ctx, `
		SELECT id, type, name, content, created_at, updated_at
		FROM sessions WHERE id = $1
	`, id).Scan(&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, internalError("Failed to get session", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// CreateSession creates a new session
func CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
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

	var session Session
	err := config.PgPool.QueryRow(ctx, `
		INSERT INTO sessions (id, type, name, content)
		VALUES ($1, $2, $3, $4)
		RETURNING id, type, name, content, created_at, updated_at
	`, req.ID, req.Type, req.Name, req.Content).Scan(
		&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt,
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

// UpdateSession updates an existing session (full replace of name and content)
func UpdateSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == nil {
		req.Content = json.RawMessage("[]")
	}

	ctx := r.Context()

	var session Session
	err = config.PgPool.QueryRow(ctx, `
		UPDATE sessions
		SET name = $2, content = $3
		WHERE id = $1
		RETURNING id, type, name, content, created_at, updated_at
	`, id, req.Name, req.Content).Scan(
		&session.ID, &session.Type, &session.Name, &session.Content, &session.CreatedAt, &session.UpdatedAt,
	)
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

// DeleteSession deletes a session by ID
func DeleteSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	result, err := config.PgPool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
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

// SessionLock represents a lock on a session
type SessionLock struct {
	SessionID string     `json:"session_id"`
	LockID    string     `json:"lock_id"`
	Until     time.Time  `json:"until"`
	Question  string     `json:"question,omitempty"`
}

// AcquireLockRequest is the request body for acquiring a lock
type AcquireLockRequest struct {
	LockID   string `json:"lock_id"`
	Duration int    `json:"duration_seconds"` // How long to hold the lock (max 300s)
	Question string `json:"question,omitempty"`
}

// GetSessionLock returns the current lock status for a session
func GetSessionLock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var lockID, question *string
	var lockUntil *time.Time
	err = config.PgPool.QueryRow(ctx, `
		SELECT lock_id, lock_until, lock_question FROM sessions WHERE id = $1
	`, id).Scan(&lockID, &lockUntil, &question)
	if err != nil {
		if err.Error() == "no rows in result set" {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, internalError("Failed to get session lock", err), http.StatusInternalServerError)
		return
	}

	// Check if lock is active
	if lockID == nil || lockUntil == nil || lockUntil.Before(time.Now()) {
		// No active lock
		w.WriteHeader(http.StatusNoContent)
		return
	}

	q := ""
	if question != nil {
		q = *question
	}

	lock := SessionLock{
		SessionID: id.String(),
		LockID:    *lockID,
		Until:     *lockUntil,
		Question:  q,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lock)
}

// AcquireSessionLock attempts to acquire a lock on a session
func AcquireSessionLock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	var req AcquireLockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.LockID == "" {
		http.Error(w, "lock_id is required", http.StatusBadRequest)
		return
	}

	// Limit lock duration to 5 minutes
	if req.Duration <= 0 || req.Duration > 300 {
		req.Duration = 60 // Default 1 minute
	}

	ctx := r.Context()
	lockUntil := time.Now().Add(time.Duration(req.Duration) * time.Second)

	// Try to acquire lock - only succeeds if no lock exists or lock is expired or we own it
	result, err := config.PgPool.Exec(ctx, `
		UPDATE sessions
		SET lock_id = $2, lock_until = $3, lock_question = $4
		WHERE id = $1
		AND (lock_id IS NULL OR lock_until < NOW() OR lock_id = $2)
	`, id, req.LockID, lockUntil, req.Question)
	if err != nil {
		http.Error(w, internalError("Failed to acquire lock", err), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		// Lock is held by someone else - return the current lock info
		var lockID, question *string
		var existingLockUntil *time.Time
		err = config.PgPool.QueryRow(ctx, `
			SELECT lock_id, lock_until, lock_question FROM sessions WHERE id = $1
		`, id).Scan(&lockID, &existingLockUntil, &question)
		if err != nil {
			if err.Error() == "no rows in result set" {
				// Session doesn't exist on server yet (only in browser localStorage).
				// No other browser can have a lock on it, so return success.
				// The session will be created when it's synced after the response completes.
				lock := SessionLock{
					SessionID: id.String(),
					LockID:    req.LockID,
					Until:     lockUntil,
					Question:  req.Question,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(lock)
				return
			}
			http.Error(w, internalError("Failed to check lock", err), http.StatusInternalServerError)
			return
		}

		q := ""
		if question != nil {
			q = *question
		}

		lock := SessionLock{
			SessionID: id.String(),
			LockID:    *lockID,
			Until:     *existingLockUntil,
			Question:  q,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(lock)
		return
	}

	// Lock acquired successfully
	lock := SessionLock{
		SessionID: id.String(),
		LockID:    req.LockID,
		Until:     lockUntil,
		Question:  req.Question,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lock)
}

// ReleaseSessionLock releases a lock on a session
func ReleaseSessionLock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	lockID := r.URL.Query().Get("lock_id")
	if lockID == "" {
		http.Error(w, "lock_id query parameter is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Only release if we own the lock
	_, err = config.PgPool.Exec(ctx, `
		UPDATE sessions
		SET lock_id = NULL, lock_until = NULL, lock_question = NULL
		WHERE id = $1 AND lock_id = $2
	`, id, lockID)
	if err != nil {
		http.Error(w, internalError("Failed to release lock", err), http.StatusInternalServerError)
		return
	}

	// If no rows affected, either session doesn't exist or we don't own the lock.
	// Either way, the end state is "we don't have a lock" which is what we want.
	// Return success (no-op for non-existent sessions).
	w.WriteHeader(http.StatusNoContent)
}

// WatchSessionLock streams lock status changes via SSE
func WatchSessionLock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Get the client's lock_id to exclude from notifications
	clientLockID := r.URL.Query().Get("lock_id")

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastLockID *string

	// Helper to get current lock state
	getLockState := func() (*string, *time.Time, *string, error) {
		var lockID, question *string
		var lockUntil *time.Time
		err := config.PgPool.QueryRow(ctx, `
			SELECT lock_id, lock_until, lock_question FROM sessions WHERE id = $1
		`, id).Scan(&lockID, &lockUntil, &question)
		return lockID, lockUntil, question, err
	}

	// Send initial state (if session exists)
	lockID, lockUntil, question, err := getLockState()
	if err != nil {
		if err.Error() == "no rows in result set" {
			// Session doesn't exist yet - just keep connection open.
			// No other browser can have a lock on it, so nothing to watch.
			// The ticker loop will pick up changes if/when the session is created.
			lockID = nil
		} else {
			http.Error(w, internalError("Failed to get lock", err), http.StatusInternalServerError)
			return
		}
	}

	// Send initial lock state (if locked by someone else)
	if lockID != nil && *lockID != clientLockID && lockUntil != nil && lockUntil.After(time.Now()) {
		q := ""
		if question != nil {
			q = *question
		}
		data, _ := json.Marshal(SessionLock{
			SessionID: id.String(),
			LockID:    *lockID,
			Until:     *lockUntil,
			Question:  q,
		})
		fmt.Fprintf(w, "event: locked\ndata: %s\n\n", data)
		flusher.Flush()
	}
	lastLockID = lockID

	// Watch for changes
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lockID, lockUntil, question, err := getLockState()
			if err != nil {
				continue // Ignore transient errors
			}

			// Detect changes
			wasLocked := lastLockID != nil && *lastLockID != clientLockID
			isLocked := lockID != nil && *lockID != clientLockID && lockUntil != nil && lockUntil.After(time.Now())

			if !wasLocked && isLocked {
				// Lock acquired by someone else
				q := ""
				if question != nil {
					q = *question
				}
				data, _ := json.Marshal(SessionLock{
					SessionID: id.String(),
					LockID:    *lockID,
					Until:     *lockUntil,
					Question:  q,
				})
				fmt.Fprintf(w, "event: locked\ndata: %s\n\n", data)
				flusher.Flush()
			} else if wasLocked && !isLocked {
				// Lock released
				fmt.Fprintf(w, "event: unlocked\ndata: {}\n\n")
				flusher.Flush()
			}

			lastLockID = lockID
		}
	}
}
