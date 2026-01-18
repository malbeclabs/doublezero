// Simple Cypher query formatter
// Formats Cypher queries for better readability with proper indentation and keyword casing

const CYPHER_KEYWORDS = [
  'MATCH', 'OPTIONAL MATCH', 'WHERE', 'RETURN', 'WITH', 'ORDER BY', 'SKIP', 'LIMIT',
  'CREATE', 'MERGE', 'DELETE', 'DETACH DELETE', 'REMOVE', 'SET', 'ON CREATE SET', 'ON MATCH SET',
  'UNWIND', 'FOREACH', 'CALL', 'YIELD', 'UNION', 'UNION ALL',
  'AND', 'OR', 'NOT', 'XOR', 'IN', 'STARTS WITH', 'ENDS WITH', 'CONTAINS',
  'IS NULL', 'IS NOT NULL', 'AS', 'DISTINCT', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END',
]

// Keywords that should start on a new line (main clauses)
const NEWLINE_KEYWORDS = [
  'MATCH', 'OPTIONAL MATCH', 'WHERE', 'RETURN', 'WITH', 'ORDER BY',
  'CREATE', 'MERGE', 'DELETE', 'DETACH DELETE', 'SET', 'REMOVE',
  'UNWIND', 'FOREACH', 'CALL', 'UNION', 'UNION ALL',
  'ON CREATE SET', 'ON MATCH SET',
]

// Detect if a query is Cypher (vs SQL)
export function isCypherQuery(query: string): boolean {
  const trimmed = query.trim().toUpperCase()
  // Cypher queries typically start with these keywords
  const cypherStarters = ['MATCH', 'CREATE', 'MERGE', 'OPTIONAL', 'CALL', 'UNWIND', 'WITH']
  // Check if starts with a Cypher keyword
  for (const starter of cypherStarters) {
    if (trimmed.startsWith(starter + ' ') || trimmed.startsWith(starter + '(') || trimmed === starter) {
      return true
    }
  }
  // Also check for graph pattern syntax: (node) or -[rel]->
  if (/\([a-z_][a-z0-9_]*\s*:/i.test(query) || /\[[a-z_][a-z0-9_]*\s*:/i.test(query)) {
    return true
  }
  // Check for relationship patterns like -[:TYPE]-> or -[r:TYPE]->
  if (/-\[.*\]->|<-\[.*\]-/.test(query)) {
    return true
  }
  return false
}

// Format a Cypher query for display
export function formatCypher(query: string): string {
  if (!query.trim()) return query

  try {
    let result = query.trim()

    // Normalize whitespace (but preserve strings)
    result = normalizeWhitespace(result)

    // Uppercase keywords
    result = uppercaseKeywords(result)

    // Add newlines before main clauses
    result = addNewlines(result)

    // Clean up extra whitespace
    result = result
      .split('\n')
      .map(line => line.trim())
      .filter((line, i, arr) => line || (i > 0 && arr[i - 1])) // Remove consecutive empty lines
      .join('\n')

    return result
  } catch {
    // If formatting fails, return original
    return query
  }
}

function normalizeWhitespace(query: string): string {
  // Simple approach: collapse multiple spaces/newlines to single space
  // This is a simplified version that doesn't perfectly handle strings
  // but works well enough for display purposes
  return query.replace(/\s+/g, ' ').trim()
}

function uppercaseKeywords(query: string): string {
  let result = query

  // Sort keywords by length descending to match longer ones first
  const sortedKeywords = [...CYPHER_KEYWORDS].sort((a, b) => b.length - a.length)

  for (const keyword of sortedKeywords) {
    // Match keyword as a whole word (not part of another word)
    const regex = new RegExp(`\\b${escapeRegex(keyword)}\\b`, 'gi')
    result = result.replace(regex, keyword)
  }

  return result
}

function addNewlines(query: string): string {
  let result = query

  // Sort by length descending to match longer patterns first
  const sortedKeywords = [...NEWLINE_KEYWORDS].sort((a, b) => b.length - a.length)

  for (const keyword of sortedKeywords) {
    // Add newline before keyword (if not at start)
    const regex = new RegExp(`(?<!^)\\s+\\b(${escapeRegex(keyword)})\\b`, 'gi')
    result = result.replace(regex, `\n$1`)
  }

  return result
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
