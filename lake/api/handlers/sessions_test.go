package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestAccount creates a test account in the database and returns it
func createTestAccount(t *testing.T, ctx context.Context) *handlers.Account {
	t.Helper()
	account := &handlers.Account{
		ID:          uuid.New(),
		AccountType: "wallet",
		IsActive:    true,
	}
	walletAddr := "test_wallet_" + uuid.New().String()[:8]
	account.WalletAddress = &walletAddr

	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO accounts (id, account_type, wallet_address, is_active)
		VALUES ($1, $2, $3, $4)
	`, account.ID, account.AccountType, account.WalletAddress, account.IsActive)
	require.NoError(t, err)

	return account
}

// withAccount creates a request with an account in context
func withAccount(r *http.Request, account *handlers.Account) *http.Request {
	ctx := handlers.SetAccountInContext(r.Context(), account)
	return r.WithContext(ctx)
}

// withChiURLParams adds chi URL parameters to a request
func withChiURLParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateSession_Authenticated(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	sessionID := uuid.New()
	reqBody := handlers.CreateSessionRequestWithOwner{
		ID:      sessionID,
		Type:    "chat",
		Name:    strPtr("Test Session"),
		Content: json.RawMessage(`[]`),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.CreateSession(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var session handlers.Session
	err := json.NewDecoder(rr.Body).Decode(&session)
	require.NoError(t, err)

	assert.Equal(t, sessionID, session.ID)
	assert.Equal(t, "chat", session.Type)
	assert.NotNil(t, session.Name)
	assert.Equal(t, "Test Session", *session.Name)
	assert.Equal(t, account.ID, *session.AccountID)
}

func TestCreateSession_Anonymous(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)

	sessionID := uuid.New()
	anonymousID := "anon_" + uuid.New().String()[:8]
	reqBody := handlers.CreateSessionRequestWithOwner{
		ID:          sessionID,
		Type:        "query",
		Content:     json.RawMessage(`[]`),
		AnonymousID: &anonymousID,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.CreateSession(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)

	var session handlers.Session
	err := json.NewDecoder(rr.Body).Decode(&session)
	require.NoError(t, err)

	assert.Equal(t, sessionID, session.ID)
	assert.Equal(t, "query", session.Type)
	assert.NotNil(t, session.AnonymousID)
	assert.Equal(t, anonymousID, *session.AnonymousID)
}

func TestCreateSession_NoAuth(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)

	sessionID := uuid.New()
	reqBody := handlers.CreateSessionRequestWithOwner{
		ID:      sessionID,
		Type:    "chat",
		Content: json.RawMessage(`[]`),
		// No account in context, no anonymous_id
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.CreateSession(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestCreateSession_InvalidType(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	sessionID := uuid.New()
	reqBody := handlers.CreateSessionRequestWithOwner{
		ID:      sessionID,
		Type:    "invalid",
		Content: json.RawMessage(`[]`),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.CreateSession(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetSession_Owner(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create a session directly in DB
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content, account_id)
		VALUES ($1, 'chat', 'Test Session', '[]', $2)
	`, sessionID, account.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String(), nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.GetSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var session handlers.Session
	err = json.NewDecoder(rr.Body).Decode(&session)
	require.NoError(t, err)
	assert.Equal(t, sessionID, session.ID)
}

func TestGetSession_NotFound(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+uuid.New().String(), nil)
	req = withChiURLParams(req, map[string]string{"id": uuid.New().String()})
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.GetSession(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetSession_Forbidden(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	owner := createTestAccount(t, ctx)
	otherUser := createTestAccount(t, ctx)

	// Create a session owned by owner
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content, account_id)
		VALUES ($1, 'chat', 'Test Session', '[]', $2)
	`, sessionID, owner.ID)
	require.NoError(t, err)

	// Try to access as otherUser
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID.String(), nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})
	req = withAccount(req, otherUser)

	rr := httptest.NewRecorder()
	handlers.GetSession(rr, req)

	// Should return 404 (not 403) to avoid leaking existence
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdateSession(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content, account_id)
		VALUES ($1, 'chat', 'Original Name', '[]', $2)
	`, sessionID, account.ID)
	require.NoError(t, err)

	// Update the session
	reqBody := handlers.UpdateSessionRequestWithOwner{
		Name:    strPtr("Updated Name"),
		Content: json.RawMessage(`[{"role":"user","content":"test"}]`),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/sessions/"+sessionID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.UpdateSession(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var session handlers.Session
	err = json.NewDecoder(rr.Body).Decode(&session)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", *session.Name)
}

func TestDeleteSession(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create a session
	sessionID := uuid.New()
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, name, content, account_id)
		VALUES ($1, 'chat', 'Test Session', '[]', $2)
	`, sessionID, account.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sessionID.String(), nil)
	req = withChiURLParams(req, map[string]string{"id": sessionID.String()})
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.DeleteSession(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Verify session is deleted
	var count int
	err = config.PgPool.QueryRow(ctx, "SELECT COUNT(*) FROM sessions WHERE id = $1", sessionID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestListSessions_Pagination(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create 5 sessions
	for i := 0; i < 5; i++ {
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO sessions (id, type, name, content, account_id)
			VALUES ($1, 'chat', $2, '[]', $3)
		`, uuid.New(), "Session "+string(rune('A'+i)), account.ID)
		require.NoError(t, err)
	}

	// Request first page (limit 2)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?type=chat&limit=2", nil)
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.ListSessions(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SessionListResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Sessions, 2)
	assert.Equal(t, 5, response.Total)
	assert.True(t, response.HasMore)

	// Request second page
	req = httptest.NewRequest(http.MethodGet, "/api/sessions?type=chat&limit=2&offset=2", nil)
	req = withAccount(req, account)

	rr = httptest.NewRecorder()
	handlers.ListSessions(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Sessions, 2)
	assert.True(t, response.HasMore)
}

func TestListSessions_TypeFilter(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create chat and query sessions
	_, err := config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, content, account_id)
		VALUES ($1, 'chat', '[]', $2)
	`, uuid.New(), account.ID)
	require.NoError(t, err)

	_, err = config.PgPool.Exec(ctx, `
		INSERT INTO sessions (id, type, content, account_id)
		VALUES ($1, 'query', '[]', $2)
	`, uuid.New(), account.ID)
	require.NoError(t, err)

	// List chat sessions
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?type=chat", nil)
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.ListSessions(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SessionListResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 1, response.Total)

	// List query sessions
	req = httptest.NewRequest(http.MethodGet, "/api/sessions?type=query", nil)
	req = withAccount(req, account)

	rr = httptest.NewRecorder()
	handlers.ListSessions(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 1, response.Total)
}

func TestBatchGetSessions(t *testing.T) {
	apitesting.SetupTestDB(t, testPgDB)
	ctx := t.Context()

	account := createTestAccount(t, ctx)

	// Create 3 sessions
	ids := make([]uuid.UUID, 3)
	for i := 0; i < 3; i++ {
		ids[i] = uuid.New()
		_, err := config.PgPool.Exec(ctx, `
			INSERT INTO sessions (id, type, content, account_id)
			VALUES ($1, 'chat', '[]', $2)
		`, ids[i], account.ID)
		require.NoError(t, err)
	}

	// Batch get 2 of them
	reqBody := handlers.BatchGetSessionsRequestWithOwner{
		IDs: ids[:2],
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAccount(req, account)

	rr := httptest.NewRecorder()
	handlers.BatchGetSessions(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.BatchGetSessionsResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Sessions, 2)
}

func strPtr(s string) *string {
	return &s
}
