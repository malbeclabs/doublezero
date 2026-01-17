package indexer

import "embed"

//go:embed db/clickhouse/migrations/*.sql
var ClickHouseMigrationsFS embed.FS

//go:embed db/neo4j/migrations/*.cypher
var Neo4jMigrationsFS embed.FS
