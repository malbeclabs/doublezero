package schema

type Schema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Tables      []TableInfo `json:"tables"`
}

type TableInfo struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
}

type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}
