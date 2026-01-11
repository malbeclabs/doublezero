package react

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// OllamaAgent implements LLMClient for Ollama.
type OllamaAgent struct {
	baseURL         string
	httpClient      *http.Client
	model           string
	maxOutputTokens int64
	system          string
}

// ollamaToolUseDirective is prepended to system prompts to strongly encourage tool usage.
// Local models need explicit instructions to use tools reliably.
const ollamaToolUseDirective = `CRITICAL INSTRUCTION: You MUST use the 'query' tool to answer questions. DO NOT ask for clarification. DO NOT say you need more information. ALWAYS execute a SQL query using the tool.

When you receive an error from a query:
1. Analyze the error message
2. Fix the SQL query
3. Execute the corrected query using the tool
4. NEVER give up or ask for help - keep trying until you get results

You have access to tools. USE THEM. Every response should include a tool call unless you are providing a final answer based on query results you already received.

---

`

// NewOllamaAgent creates a new Ollama LLM client.
func NewOllamaAgent(baseURL string, model string, maxOutputTokens int64, system string) LLMClient {
	// Prepend tool-use directive to help local models use tools reliably
	enhancedSystem := ollamaToolUseDirective + system

	return &OllamaAgent{
		baseURL:         baseURL,
		httpClient:      &http.Client{Timeout: 0}, // No timeout for streaming
		model:           model,
		maxOutputTokens: maxOutputTokens,
		system:          enhancedSystem,
	}
}

// NewOllamaAgentWithHTTPClient creates a new Ollama LLM client with a custom HTTP client.
func NewOllamaAgentWithHTTPClient(baseURL string, httpClient *http.Client, model string, maxOutputTokens int64, system string) LLMClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}
	// Prepend tool-use directive to help local models use tools reliably
	enhancedSystem := ollamaToolUseDirective + system

	return &OllamaAgent{
		baseURL:         baseURL,
		httpClient:      httpClient,
		model:           model,
		maxOutputTokens: maxOutputTokens,
		system:          enhancedSystem,
	}
}

// Call sends messages to Ollama and returns a response.
func (a *OllamaAgent) Call(ctx context.Context, messages []Message, tools []Tool) (Response, error) {
	// Convert messages to Ollama format
	ollamaMsgs := make([]ollamaMessage, 0, len(messages)+1) // +1 for system message

	// Add system message first if present
	if a.system != "" {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{
			Role:    "system",
			Content: a.system,
		})
	}

	// Convert each message
	for _, msg := range messages {
		converted := msg.ToParam()
		switch m := converted.(type) {
		case ollamaMessage:
			ollamaMsgs = append(ollamaMsgs, m)
		default:
			// Try to convert from generic format
			if om, ok := a.tryConvertMessage(converted); ok {
				ollamaMsgs = append(ollamaMsgs, om)
			}
		}
	}

	// Convert tools to Ollama format
	ollamaTools := toOllamaTools(tools)

	req := ollamaChatRequest{
		Model:    a.model,
		Messages: ollamaMsgs,
		Tools:    ollamaTools,
		Stream:   false,
		Options: map[string]any{
			"num_predict": a.maxOutputTokens,
		},
	}

	resp, err := a.chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	// Collect tool names for text-based extraction fallback
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		toolNames = append(toolNames, t.Name)
	}

	return ollamaResponse{msg: resp.Message, toolNames: toolNames}, nil
}

// tryConvertMessage attempts to convert a generic message format to ollamaMessage.
func (a *OllamaAgent) tryConvertMessage(msg any) (ollamaMessage, bool) {
	// Handle map-based messages (e.g., from JSON deserialization)
	if m, ok := msg.(map[string]any); ok {
		om := ollamaMessage{}
		if role, ok := m["role"].(string); ok {
			om.Role = role
		}
		if content, ok := m["content"].(string); ok {
			om.Content = content
		}
		if name, ok := m["name"].(string); ok {
			om.Name = name
		}
		return om, om.Role != ""
	}
	return ollamaMessage{}, false
}

// chat performs the HTTP request to Ollama's chat endpoint.
func (a *OllamaAgent) chat(ctx context.Context, req ollamaChatRequest) (ollamaChatResponse, error) {
	var out ollamaChatResponse

	b, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("json marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return out, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return out, fmt.Errorf("ollama chat http %d: %s", resp.StatusCode, string(body))
	}

	// Ollama may return streaming responses (newline-delimited JSON)
	// Even when stream=false, it may still send multiple chunks
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var last ollamaChatResponse
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return out, fmt.Errorf("stream decode: %w (line=%q)", err, string(line))
		}
		if chunk.Error != "" {
			return out, fmt.Errorf("ollama error: %s", chunk.Error)
		}
		// Accumulate content from streaming chunks
		if chunk.Message.Content != "" {
			last.Message.Content += chunk.Message.Content
		}
		if len(chunk.Message.ToolCalls) > 0 {
			last.Message.ToolCalls = chunk.Message.ToolCalls
		}
		if chunk.Message.Role != "" {
			last.Message.Role = chunk.Message.Role
		}
		if chunk.Model != "" {
			last.Model = chunk.Model
		}
		last.Done = chunk.Done
		if chunk.Done {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("scan: %w", err)
	}

	return last, nil
}

// ConvertToMessage converts a generic message to an Ollama Message.
func (a *OllamaAgent) ConvertToMessage(msg any) Message {
	switch m := msg.(type) {
	case ollamaMessage:
		return OllamaMessage{Msg: m}
	case map[string]any:
		om := ollamaMessage{}
		if role, ok := m["role"].(string); ok {
			om.Role = role
		}
		if content, ok := m["content"].(string); ok {
			om.Content = content
		}
		if name, ok := m["name"].(string); ok {
			om.Name = name
		}
		return OllamaMessage{Msg: om}
	default:
		return OllamaMessage{Msg: ollamaMessage{}}
	}
}

// ConvertToolResults converts tool results to Ollama messages.
func (a *OllamaAgent) ConvertToolResults(toolUses []ToolUse, results []ToolResult) ([]Message, error) {
	msgs := make([]Message, 0, len(results))
	for i, result := range results {
		// Ollama expects tool results as separate messages with role "tool"
		// Include the tool name for proper correlation
		toolResultMsg := ollamaMessage{
			Role:    "tool",
			Content: result.Content,
		}
		// Add the tool name if we have a corresponding tool use
		if i < len(toolUses) {
			toolResultMsg.Name = toolUses[i].Name
		}
		msgs = append(msgs, OllamaMessage{Msg: toolResultMsg})
	}
	return msgs, nil
}

// CreateUserMessage creates a user message in Ollama format.
func (a *OllamaAgent) CreateUserMessage(content string) Message {
	return NewOllamaUserMessage(content)
}

// OllamaMessage wraps Ollama's message format to implement react.Message.
type OllamaMessage struct {
	Msg ollamaMessage
}

func (m OllamaMessage) ToParam() any {
	return m.Msg
}

// ollamaResponse wraps Ollama's response to implement react.Response.
type ollamaResponse struct {
	msg       ollamaMessage
	toolNames []string // Available tool names for text-based extraction
}

func (r ollamaResponse) Content() []ContentBlock {
	blocks := make([]ContentBlock, 0)

	// Add tool calls if present (native tool calling)
	for _, tc := range r.msg.ToolCalls {
		blocks = append(blocks, ollamaContentBlock{
			blockType: "tool_use",
			toolCall:  tc,
		})
	}

	// If no native tool calls, try to extract from text content
	// This is a fallback for models that don't support native tool calling
	if len(r.msg.ToolCalls) == 0 && r.msg.Content != "" {
		extractedCalls := extractToolCallsFromText(r.msg.Content, r.toolNames)
		for _, tc := range extractedCalls {
			blocks = append(blocks, ollamaContentBlock{
				blockType: "tool_use",
				toolCall:  tc,
			})
		}

		// If we extracted tool calls, only include text that's not the tool call JSON
		if len(extractedCalls) > 0 {
			// Strip out the JSON from the text for cleaner output
			cleanText := cleanTextFromToolCalls(r.msg.Content)
			if cleanText != "" {
				blocks = append(blocks, ollamaContentBlock{
					blockType: "text",
					text:      cleanText,
				})
			}
		} else {
			// No tool calls found, include full text
			blocks = append(blocks, ollamaContentBlock{
				blockType: "text",
				text:      r.msg.Content,
			})
		}
	} else if r.msg.Content != "" {
		// Native tool calls present, also include text content
		blocks = append(blocks, ollamaContentBlock{
			blockType: "text",
			text:      r.msg.Content,
		})
	}

	return blocks
}

// extractToolCallsFromText attempts to parse tool calls from text content.
// This is a fallback for models that output tool calls as text instead of structured data.
func extractToolCallsFromText(text string, toolNames []string) []ollamaToolCall {
	var calls []ollamaToolCall

	// Pattern 1: Look for JSON objects with "name" and "arguments" or "parameters"
	// e.g., {"name": "query", "arguments": {"sql": "SELECT ..."}}
	jsonPattern := regexp.MustCompile(`\{[^{}]*"name"\s*:\s*"(\w+)"[^{}]*(?:"arguments"|"parameters")\s*:\s*(\{[^{}]*\})[^{}]*\}`)
	matches := jsonPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			name := match[1]
			argsStr := match[2]
			if isValidToolName(name, toolNames) {
				calls = append(calls, ollamaToolCall{
					ID:   fmt.Sprintf("text_call_%s_%d", name, len(calls)),
					Type: "function",
					Function: ollamaToolCallFnPart{
						Name:      name,
						Arguments: ollamaJSONArgs(argsStr),
					},
				})
			}
		}
	}

	// Pattern 2: Look for function-call style: tool_name({"arg": "value"})
	// e.g., query({"sql": "SELECT ..."})
	funcPattern := regexp.MustCompile(`(\w+)\s*\(\s*(\{[^)]*\})\s*\)`)
	funcMatches := funcPattern.FindAllStringSubmatch(text, -1)
	for _, match := range funcMatches {
		if len(match) >= 3 {
			name := match[1]
			argsStr := match[2]
			if isValidToolName(name, toolNames) && !hasCall(calls, name) {
				// Validate JSON
				var tmp map[string]any
				if json.Unmarshal([]byte(argsStr), &tmp) == nil {
					calls = append(calls, ollamaToolCall{
						ID:   fmt.Sprintf("text_call_%s_%d", name, len(calls)),
						Type: "function",
						Function: ollamaToolCallFnPart{
							Name:      name,
							Arguments: ollamaJSONArgs(argsStr),
						},
					})
				}
			}
		}
	}

	// Pattern 3: Look for code blocks containing tool calls
	// ```json\n{"name": "query", ...}\n```
	codeBlockPattern := regexp.MustCompile("```(?:json)?\\s*\\n?([\\s\\S]*?)```")
	codeMatches := codeBlockPattern.FindAllStringSubmatch(text, -1)
	for _, match := range codeMatches {
		if len(match) >= 2 {
			codeContent := strings.TrimSpace(match[1])
			// Try to parse as a tool call object
			var toolCall struct {
				Name       string          `json:"name"`
				Arguments  json.RawMessage `json:"arguments"`
				Parameters json.RawMessage `json:"parameters"`
			}
			if json.Unmarshal([]byte(codeContent), &toolCall) == nil && toolCall.Name != "" {
				if isValidToolName(toolCall.Name, toolNames) && !hasCall(calls, toolCall.Name) {
					args := toolCall.Arguments
					if len(args) == 0 {
						args = toolCall.Parameters
					}
					if len(args) == 0 {
						args = json.RawMessage(`{}`)
					}
					calls = append(calls, ollamaToolCall{
						ID:   fmt.Sprintf("text_call_%s_%d", toolCall.Name, len(calls)),
						Type: "function",
						Function: ollamaToolCallFnPart{
							Name:      toolCall.Name,
							Arguments: ollamaJSONArgs(args),
						},
					})
				}
			}

			// Also try to parse as just arguments for known tool names
			// e.g., code block containing just {"sql": "SELECT ..."}
			var argsMap map[string]any
			if json.Unmarshal([]byte(codeContent), &argsMap) == nil {
				// Check if this looks like query arguments (has "sql" key)
				if _, hasSql := argsMap["sql"]; hasSql && isValidToolName("query", toolNames) && !hasCall(calls, "query") {
					calls = append(calls, ollamaToolCall{
						ID:   fmt.Sprintf("text_call_query_%d", len(calls)),
						Type: "function",
						Function: ollamaToolCallFnPart{
							Name:      "query",
							Arguments: ollamaJSONArgs(codeContent),
						},
					})
				}
			}
		}
	}

	// Pattern 4: Look for inline SQL after keywords like "query:" or "execute:"
	// e.g., "I'll run the following query: SELECT COUNT(*) FROM users"
	sqlPattern := regexp.MustCompile(`(?i)(?:query|execute|run|sql):\s*\n?\s*(SELECT\s+[^;]+;?)`)
	sqlMatches := sqlPattern.FindAllStringSubmatch(text, -1)
	for _, match := range sqlMatches {
		if len(match) >= 2 && isValidToolName("query", toolNames) && !hasCall(calls, "query") {
			sql := strings.TrimSpace(match[1])
			argsJSON, _ := json.Marshal(map[string]string{"sql": sql})
			calls = append(calls, ollamaToolCall{
				ID:   fmt.Sprintf("text_call_query_%d", len(calls)),
				Type: "function",
				Function: ollamaToolCallFnPart{
					Name:      "query",
					Arguments: ollamaJSONArgs(argsJSON),
				},
			})
		}
	}

	// Pattern 5: Look for SQL code blocks (```sql ... ```)
	// This catches cases where the model writes SQL in a code block
	if isValidToolName("query", toolNames) && !hasCall(calls, "query") {
		sqlBlockPattern := regexp.MustCompile("```sql\\s*\\n([\\s\\S]*?)```")
		sqlBlockMatches := sqlBlockPattern.FindAllStringSubmatch(text, -1)
		for _, match := range sqlBlockMatches {
			if len(match) >= 2 {
				sql := strings.TrimSpace(match[1])
				if sql != "" && (strings.HasPrefix(strings.ToUpper(sql), "SELECT") ||
					strings.HasPrefix(strings.ToUpper(sql), "WITH") ||
					strings.HasPrefix(strings.ToUpper(sql), "SHOW")) {
					argsJSON, _ := json.Marshal(map[string]string{"sql": sql})
					calls = append(calls, ollamaToolCall{
						ID:   fmt.Sprintf("text_call_query_%d", len(calls)),
						Type: "function",
						Function: ollamaToolCallFnPart{
							Name:      "query",
							Arguments: ollamaJSONArgs(argsJSON),
						},
					})
					break // Only extract one SQL block
				}
			}
		}
	}

	// Pattern 6: Look for any SELECT/WITH statement in the text (last resort)
	// This is aggressive but helps with models that just write SQL inline
	if isValidToolName("query", toolNames) && !hasCall(calls, "query") {
		// Look for SQL that spans multiple lines or is reasonably complete
		inlineSQLPattern := regexp.MustCompile(`(?is)((?:SELECT|WITH)\s+[\s\S]{20,}?(?:;|FROM\s+\w+[\s\S]*?(?:WHERE|GROUP|ORDER|LIMIT|;|$)))`)
		inlineMatches := inlineSQLPattern.FindStringSubmatch(text)
		if len(inlineMatches) >= 2 {
			sql := strings.TrimSpace(inlineMatches[1])
			// Clean up the SQL - remove trailing incomplete parts
			sql = strings.TrimSuffix(sql, ";")
			sql = strings.TrimSpace(sql)
			if len(sql) > 30 { // Only if it looks like a real query
				argsJSON, _ := json.Marshal(map[string]string{"sql": sql})
				calls = append(calls, ollamaToolCall{
					ID:   fmt.Sprintf("text_call_query_%d", len(calls)),
					Type: "function",
					Function: ollamaToolCallFnPart{
						Name:      "query",
						Arguments: ollamaJSONArgs(argsJSON),
					},
				})
			}
		}
	}

	return calls
}

// isValidToolName checks if a name is in the list of available tools
func isValidToolName(name string, toolNames []string) bool {
	if len(toolNames) == 0 {
		// If no tool names provided, accept any name that looks like a tool
		return name != "" && len(name) < 50
	}
	for _, tn := range toolNames {
		if tn == name {
			return true
		}
	}
	return false
}

// hasCall checks if a tool call already exists in the list
func hasCall(calls []ollamaToolCall, name string) bool {
	for _, c := range calls {
		if c.Function.Name == name {
			return true
		}
	}
	return false
}

// cleanTextFromToolCalls removes JSON tool call patterns from text
func cleanTextFromToolCalls(text string) string {
	// Remove code blocks
	text = regexp.MustCompile("```(?:json)?\\s*\\n?[\\s\\S]*?```").ReplaceAllString(text, "")
	// Remove inline JSON objects that look like tool calls
	text = regexp.MustCompile(`\{[^{}]*"name"\s*:\s*"\w+"[^{}]*\}`).ReplaceAllString(text, "")
	// Clean up extra whitespace
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func (r ollamaResponse) ToMessage() Message {
	return OllamaMessage{Msg: r.msg}
}

// ollamaContentBlock implements react.ContentBlock for Ollama.
type ollamaContentBlock struct {
	blockType string
	text      string
	toolCall  ollamaToolCall
}

func (b ollamaContentBlock) AsText() (string, bool) {
	if b.blockType == "text" && b.text != "" {
		return b.text, true
	}
	return "", false
}

func (b ollamaContentBlock) AsToolUse() (string, string, []byte, bool) {
	if b.blockType == "tool_use" {
		// Generate an ID if not present (Ollama may not provide one)
		id := b.toolCall.ID
		if id == "" {
			id = fmt.Sprintf("tool_%s", b.toolCall.Function.Name)
		}
		return id, b.toolCall.Function.Name, b.toolCall.Function.Arguments.Raw(), true
	}
	return "", "", nil, false
}

// Ollama API types

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	Name      string           `json:"name,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function ollamaToolCallFnPart `json:"function"`
}

type ollamaToolCallFnPart struct {
	Name      string         `json:"name"`
	Arguments ollamaJSONArgs `json:"arguments"`
}

// ollamaJSONArgs handles Ollama's flexible JSON argument format.
type ollamaJSONArgs json.RawMessage

func (a *ollamaJSONArgs) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*a = ollamaJSONArgs(`{}`)
		return nil
	}

	cur := bytes.TrimSpace(b)

	// Peel quotes a few times (handles escaped JSON strings)
	for i := 0; i < 4 && len(cur) > 0 && cur[0] == '"'; i++ {
		var s string
		if err := json.Unmarshal(cur, &s); err != nil {
			return err
		}
		cur = bytes.TrimSpace([]byte(s))
	}

	if len(cur) == 0 {
		*a = ollamaJSONArgs(`{}`)
		return nil
	}

	// Keep valid JSON objects/arrays as-is
	if json.Valid(cur) && (cur[0] == '{' || cur[0] == '[') {
		*a = ollamaJSONArgs(cur)
		return nil
	}

	// Otherwise, force an object
	wrapped, err := json.Marshal(map[string]any{"_raw": string(cur)})
	if err != nil {
		return err
	}
	*a = ollamaJSONArgs(wrapped)
	return nil
}

func (a ollamaJSONArgs) Raw() json.RawMessage {
	if len(a) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(a)
}

func (a ollamaJSONArgs) MarshalJSON() ([]byte, error) {
	if len(a) == 0 {
		return []byte(`{}`), nil
	}
	if !json.Valid([]byte(a)) {
		return json.Marshal(map[string]any{"_raw": string(a)})
	}
	return []byte(a), nil
}

type ollamaToolDef struct {
	Type     string              `json:"type"`
	Function ollamaToolDefFnPart `json:"function"`
}

type ollamaToolDefFnPart struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaToolDef `json:"tools,omitempty"`
	Stream   bool            `json:"stream,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model   string        `json:"model,omitempty"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// toOllamaTools converts tools to Ollama tool definitions.
func toOllamaTools(tools []Tool) []ollamaToolDef {
	out := make([]ollamaToolDef, 0, len(tools))
	for _, t := range tools {
		params, _ := json.Marshal(t.InputSchema)
		toolDef := ollamaToolDef{
			Type: "function",
			Function: ollamaToolDefFnPart{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		}
		out = append(out, toolDef)
	}
	return out
}

// Helper functions for creating Ollama messages

// NewOllamaUserMessage creates a new user message for Ollama.
func NewOllamaUserMessage(content string) OllamaMessage {
	return OllamaMessage{Msg: ollamaMessage{
		Role:    "user",
		Content: content,
	}}
}

// NewOllamaAssistantMessage creates a new assistant message for Ollama.
func NewOllamaAssistantMessage(content string) OllamaMessage {
	return OllamaMessage{Msg: ollamaMessage{
		Role:    "assistant",
		Content: content,
	}}
}

// NewOllamaToolResultMessage creates a new tool result message for Ollama.
func NewOllamaToolResultMessage(toolName, content string) OllamaMessage {
	return OllamaMessage{Msg: ollamaMessage{
		Role:    "tool",
		Name:    toolName,
		Content: content,
	}}
}
