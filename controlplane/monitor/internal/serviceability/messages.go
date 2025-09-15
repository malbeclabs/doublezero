package serviceability

import (
	"encoding/json"
	"fmt"
)

// TextObject represents a Slack text object.
// https://api.slack.com/reference/block-kit/composition-objects#text
type TextObject struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// ColumnSetting defines settings for a table column.
// https://api.slack.com/reference/block-kit/blocks#table_column_settings
type ColumnSetting struct {
	IsWrapped bool   `json:"is_wrapped,omitempty"`
	Align     string `json:"align,omitempty"`
}

// TableCell represents a cell in a Slack table.
type TableCell struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Block represents a generic component in a Slack message's blocks.
// We use omitempty for fields that are not present in all block types.
type Block struct {
	Type           string          `json:"type"`
	Text           *TextObject     `json:"text,omitempty"`
	ColumnSettings []ColumnSetting `json:"column_settings,omitempty"`
	Rows           [][]TableCell   `json:"rows,omitempty"`
}

// SlackMessage is the top-level structure for a Slack message payload.
type SlackMessage struct {
	Blocks []Block `json:"blocks"`
}

// GenerateSlackTableMessage creates a Slack message payload with a header and a table.
//
// Parameters:
//   - headerText: The text for the header block.
//   - tableRows: A slice of rows, where each row is a slice of strings for the cells.
//     The first row is typically the table header.
//   - columnSettings: An optional slice of column setting objects. If nil, default settings are used.
//
// Returns:
//   - A JSON string representing the Slack message payload.
//   - An error if the payload cannot be marshaled to JSON.
func GenerateSlackTableMessage(headerText string, tableRows [][]string, columnSettings []ColumnSetting) (string, error) {
	headerBlock := Block{
		Type: "header",
		Text: &TextObject{
			Type:  "plain_text",
			Text:  headerText,
			Emoji: true,
		},
	}

	slackRows := make([][]TableCell, len(tableRows))
	for i, row := range tableRows {
		slackRow := make([]TableCell, len(row))
		for j, cellText := range row {
			slackRow[j] = TableCell{
				Type: "raw_text",
				Text: cellText,
			}
		}
		slackRows[i] = slackRow
	}

	if columnSettings == nil {
		columnSettings = []ColumnSetting{
			{IsWrapped: true},
			{Align: "right"},
		}
	}

	tableBlock := Block{
		Type:           "table",
		ColumnSettings: columnSettings,
		Rows:           slackRows,
	}

	message := SlackMessage{
		Blocks: []Block{headerBlock, tableBlock},
	}

	payload, err := json.MarshalIndent(message, "", "\t")
	if err != nil {
		return "", fmt.Errorf("failed to marshal slack message to JSON: %w", err)
	}

	return string(payload), nil
}
