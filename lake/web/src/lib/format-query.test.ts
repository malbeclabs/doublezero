import { describe, it, expect } from 'vitest'
import { formatSqlQuery, formatQuery, formatQueryByType } from './format-query'

describe('formatSqlQuery', () => {
  it('preserves empty queries', () => {
    expect(formatSqlQuery('')).toBe('')
    expect(formatSqlQuery('   ')).toBe('   ')
  })

  it('formats simple SELECT queries', () => {
    const result = formatSqlQuery('select * from users')
    expect(result).toContain('SELECT')
    expect(result).toContain('FROM')
  })

  it('uppercases keywords', () => {
    const result = formatSqlQuery('select id, name from users where active = true')
    expect(result).toContain('SELECT')
    expect(result).toContain('FROM')
    expect(result).toContain('WHERE')
  })

  it('handles multi-line formatting', () => {
    const result = formatSqlQuery('SELECT id, name FROM users WHERE active = true ORDER BY name')
    expect(result).toContain('\n')
  })

  it('returns original on parse failure', () => {
    const malformed = 'NOTASQL;STATEMENT;THING'
    const result = formatSqlQuery(malformed)
    expect(result).toBeTruthy()
  })

  it('handles ClickHouse-style queries', () => {
    const query = 'SELECT count(*) FROM system.tables WHERE database = currentDatabase()'
    const result = formatSqlQuery(query)
    expect(result).toContain('SELECT')
    expect(result).toContain('FROM')
  })
})

describe('formatQuery', () => {
  it('detects and formats SQL queries', () => {
    const result = formatQuery('SELECT * FROM users')
    expect(result.language).toBe('sql')
    expect(result.formatted).toContain('SELECT')
  })

  it('detects and formats Cypher queries', () => {
    const result = formatQuery('MATCH (n) RETURN n')
    expect(result.language).toBe('cypher')
    expect(result.formatted).toContain('MATCH')
  })

  it('handles empty queries', () => {
    const result = formatQuery('')
    expect(result.language).toBe('sql')
    expect(result.formatted).toBe('')
  })

  it('handles whitespace-only queries', () => {
    const result = formatQuery('   ')
    expect(result.language).toBe('sql')
  })

  it('correctly identifies Cypher by graph patterns', () => {
    const result = formatQuery('MATCH (a:Person)-[:KNOWS]->(b) RETURN a, b')
    expect(result.language).toBe('cypher')
  })

  it('correctly identifies SQL with subqueries', () => {
    const result = formatQuery('SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)')
    expect(result.language).toBe('sql')
  })
})

describe('formatQueryByType', () => {
  it('formats SQL when type is sql', () => {
    const result = formatQueryByType('select * from users', 'sql')
    expect(result).toContain('SELECT')
    expect(result).toContain('FROM')
  })

  it('formats Cypher when type is cypher', () => {
    const result = formatQueryByType('match (n) return n', 'cypher')
    expect(result).toContain('MATCH')
    expect(result).toContain('RETURN')
  })

  it('preserves empty queries', () => {
    expect(formatQueryByType('', 'sql')).toBe('')
    expect(formatQueryByType('', 'cypher')).toBe('')
    expect(formatQueryByType('   ', 'sql')).toBe('   ')
    expect(formatQueryByType('   ', 'cypher')).toBe('   ')
  })

  it('respects type override regardless of content', () => {
    // SQL content but formatted as Cypher
    const sqlAsCypher = formatQueryByType('SELECT * FROM users', 'cypher')
    // Cypher formatter will uppercase keywords it recognizes
    expect(sqlAsCypher).toBeTruthy()

    // Cypher content but formatted as SQL
    const cypherAsSql = formatQueryByType('MATCH (n) RETURN n', 'sql')
    expect(cypherAsSql).toBeTruthy()
  })
})
