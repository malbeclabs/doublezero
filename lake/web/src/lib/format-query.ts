// Query formatting utilities for SQL and Cypher
import { format as formatSQL } from 'sql-formatter'
import { isCypherQuery, formatCypher } from './format-cypher'

// Format a SQL query for display
export function formatSqlQuery(sql: string): string {
  if (!sql.trim()) return sql
  try {
    // Try formatting with different SQL dialects if the default fails
    // ClickHouse has some non-standard syntax that may not parse correctly
    const dialects = ['sql', 'mysql', 'postgresql'] as const
    for (const dialect of dialects) {
      try {
        const formatted = formatSQL(sql, {
          language: dialect,
          tabWidth: 2,
          keywordCase: 'upper',
        })
        // Check if formatting actually added newlines (sign of successful parse)
        if (formatted.includes('\n')) {
          return formatted
        }
      } catch {
        // Try next dialect
      }
    }
    // If no dialect worked well, use the default result
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
