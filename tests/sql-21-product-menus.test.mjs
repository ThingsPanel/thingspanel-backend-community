import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const dir = dirname(fileURLToPath(import.meta.url))
const sql = readFileSync(join(dir, '..', 'sql', '21.sql'), 'utf8')

test('21.sql inserts product and OTA menus for SYS_ADMIN', () => {
  for (const code of ['product', 'product_list', 'product_update-package', 'product_update-ota']) {
    assert.match(sql, new RegExp(`'${code}'`))
  }
  assert.match(sql, /产品管理/)
  assert.match(sql, /OTA升级/)
  assert.match(sql, /升级包管理/)
  assert.match(sql, /SYS_ADMIN/)
  assert.match(sql, /ON CONFLICT \(id\) DO NOTHING/)
})
