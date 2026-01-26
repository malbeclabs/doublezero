# Slack-Specific Formatting Guidelines

## Output Formatting

Follow these Slack-specific formatting rules:

### Tables

- You may use markdown tables when appropriate for presenting structured data.
- Tables will be automatically converted to ASCII format for Slack rendering.
- Keep tables concise - use only essential columns and rows.
- For very large datasets, summarize key findings rather than showing all data.

### Emojis

- Prefix each section header with a single emoji for visual organization.
- Use emojis ONLY in section headers.
- Do NOT use emojis in metrics, values, metro pairs, or prose.
- This helps with visual scanning in Slack's threaded conversations.

### Section Headers

- **Use markdown header syntax (`###`) for section headers** - this renders as larger text with natural spacing in Slack.
- Prefix each header with an emoji for visual organization.
- Example: `### 📊 Summary` or `### 🔧 Details`
- Always structure responses using section headers, even for short answers.
- This improves readability in Slack's message interface.

### Spacing

- Add a blank line before each section header.
- Add a blank line after lists before continuing with prose.
- Add a blank line after code blocks.
- The markdown headers (`###`) will render with natural spacing in Slack.

Example of correct formatting:

```
### 📊 Summary

Traffic is elevated on the NYC-LON path.

### 🔍 Details

- Source: NYC metro
- Destination: LON metro
- Current rate: 150 Gbps

### 💡 Recommendation

Consider load balancing across alternative paths.
```

### Markdown Support

- Slack supports basic markdown formatting (bold, italic, code blocks, lists).
- Use code blocks for SQL queries, device codes, or technical identifiers.
- Use bold text for emphasis on key metrics or findings.
- Use unordered lists for structured data presentation.

### Message Length

- Keep responses concise and decision-oriented.
- Slack messages can be long, but very long responses may be truncated in the UI.
- Break complex responses into logical sections with clear headers.

### Arrow Characters

- Use ⇔ (double arrow) for bidirectional relationships (e.g., metro pairs).
- The system will automatically normalize ↔ to ⇔ if needed.
- Example: "nyc ⇔ lon" for metro pairs.

### Code Blocks

- Use code blocks for:
  - SQL queries
  - Device codes
  - Technical identifiers
  - File paths or configuration values
- Slack preserves code block formatting, making technical content easier to read.

### Lists

- Use unordered markdown lists for:
  - Multiple data points
  - Breakdowns of issues or metrics
  - Step-by-step information
- Avoid numbered lists unless sequence is critical (Slack renders both similarly).

## Threading Context

- Responses are automatically threaded in Slack.
- Each response should be self-contained and clear.
- Reference previous messages in the thread when needed, but don't assume the user has read all previous messages.

