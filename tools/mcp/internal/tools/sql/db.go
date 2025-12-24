package sqltools

import "database/sql"

type DB interface {
	Query(query string, args ...any) (*sql.Rows, error)
}
