import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const sql = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'sql', '23.sql'), 'utf8')

test('23.sql contains the missing product initialization data and is idempotent', () => {
  for (const value of ['PRODUCT_TYPE', 'direct', 'gateway', 'sensor', 'A100-demo', 'A100']) {
    assert.match(sql, new RegExp(value))
  }
  assert.match(sql, /ON CONFLICT \(dict_code, dict_value\) DO NOTHING/)
  assert.match(sql, /ON CONFLICT \(dict_id, language_code\) DO NOTHING/)
  assert.match(sql, /ON CONFLICT \(id\) DO NOTHING/)
})
