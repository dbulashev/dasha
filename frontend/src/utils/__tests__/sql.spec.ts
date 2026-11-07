import { describe, expect, it } from 'vitest'

import { isUtilityStatement } from '@/utils/sql'

describe('isUtilityStatement', () => {
  it.each([
    'begin',
    'BEGIN ISOLATION LEVEL SERIALIZABLE',
    'commit',
    'ROLLBACK',
    'SET search_path = app',
    'DISCARD ALL',
    'savepoint s1',
    'CREATE INDEX i ON t (a)',
    'VACUUM t',
    'COPY t FROM STDIN',
    'EXPLAIN SELECT 1',
    'CALL p()',
    'COMMIT;',
    '/* app=orders */ BEGIN',
    'COMMIT; -- done',
    'COMMIT; /* done */',
    'SELECT * INTO tmp FROM t',
    'select a, b\ninto unlogged tmp\nfrom t',
  ])('flags %s as utility', (sql) => {
    expect(isUtilityStatement(sql)).toBe(true)
  })

  it.each([
    'SELECT 1',
    'insert into t values (1)',
    'UPDATE t SET a = 1',
    'DELETE FROM t',
    'MERGE INTO t USING s ON true WHEN MATCHED THEN DO NOTHING',
    'WITH x AS (SELECT 1) SELECT * FROM x',
    'VALUES (1)',
    'TABLE t',
    '-- tag\n  SELECT 1',
    '(SELECT 1) UNION (SELECT 2)',
    "SELECT 'into' FROM t",
    'SELECT "into" FROM t',
    'SELECT a FROM t -- into',
  ])('passes %s', (sql) => {
    expect(isUtilityStatement(sql)).toBe(false)
  })

  it('leaves empty and multi-statement strings unclassified', () => {
    expect(isUtilityStatement('')).toBe(false)
    expect(isUtilityStatement('BEGIN; UPDATE t SET a = 1; COMMIT')).toBe(false)
  })

  it('leaves text cut off inside its leading comment unclassified', () => {
    expect(isUtilityStatement('/* controller=orders action=index')).toBe(false)
  })
})
