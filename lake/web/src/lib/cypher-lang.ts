import { StreamLanguage, LanguageSupport, type StreamParser } from '@codemirror/language'

// Cypher keywords
const keywords = new Set([
  'MATCH', 'RETURN', 'WHERE', 'WITH', 'OPTIONAL', 'CREATE', 'DELETE', 'SET',
  'REMOVE', 'MERGE', 'UNWIND', 'CALL', 'YIELD', 'ORDER', 'BY', 'SKIP', 'LIMIT',
  'ASC', 'DESC', 'ASCENDING', 'DESCENDING', 'AND', 'OR', 'NOT', 'XOR', 'IN',
  'AS', 'DISTINCT', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'NULL', 'TRUE',
  'FALSE', 'IS', 'STARTS', 'ENDS', 'CONTAINS', 'EXISTS', 'ALL', 'ANY', 'NONE',
  'SINGLE', 'DETACH', 'FOREACH', 'ON', 'USING', 'INDEX', 'SCAN', 'JOIN',
  'UNION', 'LOAD', 'CSV', 'FROM', 'HEADERS', 'EXPLAIN', 'PROFILE', 'CYPHER',
])

// Cypher functions
const functions = new Set([
  // Aggregation
  'AVG', 'COLLECT', 'COUNT', 'MAX', 'MIN', 'SUM', 'STDEV', 'STDEVP',
  'PERCENTILECONT', 'PERCENTILEDISC',
  // String
  'LEFT', 'LTRIM', 'REPLACE', 'REVERSE', 'RIGHT', 'RTRIM', 'SPLIT', 'SUBSTRING',
  'TOLOWER', 'TOSTRING', 'TOUPPER', 'TRIM',
  // Math
  'ABS', 'CEIL', 'FLOOR', 'RAND', 'ROUND', 'SIGN', 'SQRT',
  // List
  'HEAD', 'LAST', 'RANGE', 'REDUCE', 'SIZE', 'TAIL', 'KEYS', 'LABELS', 'NODES',
  'RELATIONSHIPS', 'TYPE', 'ID', 'COALESCE', 'PROPERTIES',
  // Path
  'LENGTH', 'SHORTESTPATH', 'ALLSHORTESTPATHS',
  // Temporal
  'DATE', 'DATETIME', 'TIME', 'DURATION', 'LOCALDATETIME', 'LOCALTIME',
  // Spatial
  'POINT', 'DISTANCE',
])

interface CypherState {
  inString: string | null
  inComment: boolean
}

const cypherParser: StreamParser<CypherState> = {
  startState: () => ({
    inString: null,
    inComment: false,
  }),

  token: (stream, state) => {
    // Handle multi-line comments
    if (state.inComment) {
      if (stream.match('*/')) {
        state.inComment = false
        return 'comment'
      }
      stream.next()
      return 'comment'
    }

    // Handle strings
    if (state.inString) {
      while (!stream.eol()) {
        const ch = stream.next()
        if (ch === state.inString) {
          // Check for escaped quote
          if (stream.peek() === state.inString) {
            stream.next()
          } else {
            state.inString = null
            return 'string'
          }
        }
      }
      return 'string'
    }

    // Skip whitespace
    if (stream.eatSpace()) {
      return null
    }

    // Single-line comment
    if (stream.match('//')) {
      stream.skipToEnd()
      return 'comment'
    }

    // Multi-line comment start
    if (stream.match('/*')) {
      state.inComment = true
      return 'comment'
    }

    // String start
    const ch = stream.peek()
    if (ch === '"' || ch === "'") {
      stream.next()
      state.inString = ch
      return 'string'
    }

    // Node labels (:Label)
    if (stream.match(/:\s*[A-Za-z_][A-Za-z0-9_]*/)) {
      return 'typeName'
    }

    // Relationship types in brackets ([:REL_TYPE])
    if (stream.match(/\[\s*:\s*[A-Za-z_][A-Za-z0-9_]*/)) {
      return 'typeName'
    }

    // Parameter ($param or {param})
    if (stream.match(/\$[A-Za-z_][A-Za-z0-9_]*/)) {
      return 'variableName.special'
    }

    // Numbers
    if (stream.match(/0x[0-9a-fA-F]+/) || stream.match(/\d+\.?\d*(?:[eE][+-]?\d+)?/)) {
      return 'number'
    }

    // Keywords and functions
    if (stream.match(/[A-Za-z_][A-Za-z0-9_]*/)) {
      const word = stream.current().toUpperCase()
      if (keywords.has(word)) {
        return 'keyword'
      }
      if (functions.has(word)) {
        return 'function'
      }
      return 'variableName'
    }

    // Operators
    if (stream.match(/[+\-*/%^=<>!]+/)) {
      return 'operator'
    }

    // Brackets and punctuation
    if (stream.match(/[()[\]{}.,;|]/)) {
      return 'punctuation'
    }

    // Consume unknown character
    stream.next()
    return null
  },
}

export const cypherLanguage = StreamLanguage.define(cypherParser)

export function cypher(): LanguageSupport {
  return new LanguageSupport(cypherLanguage)
}
