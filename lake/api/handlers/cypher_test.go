package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteCypher_Match(t *testing.T) {
	// Seed some test data
	seedFunc := func(ctx context.Context, session neo4j.Session) error {
		// Create some test nodes
		_, err := session.Run(ctx, `
			CREATE (n1:TestNode {name: 'Node1', value: 100})
			CREATE (n2:TestNode {name: 'Node2', value: 200})
			CREATE (n1)-[:CONNECTS_TO]->(n2)
		`, nil)
		if err != nil {
			return err
		}
		return nil
	}

	apitesting.SetupTestNeo4jWithData(t, testNeo4jDB, seedFunc)

	reqBody := handlers.CypherQueryRequest{
		Query: "MATCH (n:TestNode) RETURN n.name as name, n.value as value ORDER BY n.name",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 2, response.RowCount)
	assert.Contains(t, response.Columns, "name")
	assert.Contains(t, response.Columns, "value")
	assert.True(t, response.ElapsedMs >= 0)
}

func TestExecuteCypher_EmptyQuery(t *testing.T) {
	apitesting.SetupTestNeo4j(t, testNeo4jDB)

	reqBody := handlers.CypherQueryRequest{
		Query: "",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteCypher_WhitespaceOnly(t *testing.T) {
	apitesting.SetupTestNeo4j(t, testNeo4jDB)

	reqBody := handlers.CypherQueryRequest{
		Query: "   \t\n  ",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteCypher_InvalidQuery(t *testing.T) {
	apitesting.SetupTestNeo4j(t, testNeo4jDB)

	reqBody := handlers.CypherQueryRequest{
		Query: "THIS IS NOT VALID CYPHER",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	// Returns 200 OK with error in response
	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.NotEmpty(t, response.Error)
}

func TestExecuteCypher_InvalidRequestBody(t *testing.T) {
	apitesting.SetupTestNeo4j(t, testNeo4jDB)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteCypher_NoNeo4j(t *testing.T) {
	// Don't set up Neo4j - test graceful fallback
	oldClient := config.Neo4jClient
	config.Neo4jClient = nil
	defer func() { config.Neo4jClient = oldClient }()

	reqBody := handlers.CypherQueryRequest{
		Query: "MATCH (n) RETURN n",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "Neo4j is not available", response.Error)
}

func TestExecuteCypher_EmptyResult(t *testing.T) {
	apitesting.SetupTestNeo4j(t, testNeo4jDB)

	// Query for non-existent nodes
	reqBody := handlers.CypherQueryRequest{
		Query: "MATCH (n:NonExistentLabel) RETURN n",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 0, response.RowCount)
}

func TestExecuteCypher_RelationshipQuery(t *testing.T) {
	// Seed data with relationships
	seedFunc := func(ctx context.Context, session neo4j.Session) error {
		_, err := session.Run(ctx, `
			CREATE (a:Person {name: 'Alice'})
			CREATE (b:Person {name: 'Bob'})
			CREATE (c:Person {name: 'Charlie'})
			CREATE (a)-[:KNOWS {since: 2020}]->(b)
			CREATE (b)-[:KNOWS {since: 2021}]->(c)
		`, nil)
		return err
	}

	apitesting.SetupTestNeo4jWithData(t, testNeo4jDB, seedFunc)

	reqBody := handlers.CypherQueryRequest{
		Query: "MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN a.name as from, b.name as to, r.since as since ORDER BY a.name",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 2, response.RowCount)
	assert.Contains(t, response.Columns, "from")
	assert.Contains(t, response.Columns, "to")
	assert.Contains(t, response.Columns, "since")
}

func TestExecuteCypher_CountQuery(t *testing.T) {
	// Seed some data
	seedFunc := func(ctx context.Context, session neo4j.Session) error {
		_, err := session.Run(ctx, `
			UNWIND range(1, 5) AS i
			CREATE (:CountNode {id: i})
		`, nil)
		return err
	}

	apitesting.SetupTestNeo4jWithData(t, testNeo4jDB, seedFunc)

	reqBody := handlers.CypherQueryRequest{
		Query: "MATCH (n:CountNode) RETURN count(n) as total",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/cypher", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteCypher(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CypherQueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 1, response.RowCount)
	require.Len(t, response.Rows, 1)
	// The count should be 5
	total, ok := response.Rows[0]["total"]
	assert.True(t, ok)
	assert.Equal(t, float64(5), total) // JSON numbers are float64
}

func TestCypherQueryRequest_Structure(t *testing.T) {
	req := handlers.CypherQueryRequest{
		Query: "MATCH (n) RETURN n LIMIT 10",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded handlers.CypherQueryRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, req.Query, decoded.Query)
}

func TestCypherQueryResponse_Structure(t *testing.T) {
	response := handlers.CypherQueryResponse{
		Columns:   []string{"name", "value"},
		Rows:      []map[string]any{{"name": "test", "value": 42}},
		RowCount:  1,
		ElapsedMs: 10,
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	var decoded handlers.CypherQueryResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, response.Columns, decoded.Columns)
	assert.Equal(t, response.RowCount, decoded.RowCount)
	assert.Equal(t, response.ElapsedMs, decoded.ElapsedMs)
}
