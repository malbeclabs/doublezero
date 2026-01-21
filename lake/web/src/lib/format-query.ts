// Query formatting utilities for SQL and Cypher
import { format as formatSQL } from 'sql-formatter'
import { isCypherQuery, formatCypher } from './format-cypher'

// Format a SQL query for display
export function formatSqlQuery(sql: string): string {
  if (!sql.trim()) return sql
  try {
    return formatSQL(sql, {
      language: 'sql',
      tabWidth: 2,
      keywordCase: 'upper',
    })
  } catch {
    return sql
  }
}

// Format a query (SQL or Cypher) for display
// Returns formatted text and detected language
export function formatQuery(query: string): { formatted: string; language: 'sql' | 'cypher' } {
  if (!query.trim()) {
    return { formatted: query, language: 'sql' }
  }

  if (isCypherQuery(query)) {
    return { formatted: formatCypher(query), language: 'cypher' }
  }
  return { formatted: formatSqlQuery(query), language: 'sql' }
}

// Format a query of known type
export function formatQueryByType(query: string, type: 'sql' | 'cypher'): string {
  if (!query.trim()) return query

  if (type === 'cypher') {
    return formatCypher(query)
  }
  return formatSqlQuery(query)
}
