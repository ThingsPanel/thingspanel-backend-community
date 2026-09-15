import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const sql = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'sql', '22.sql'), 'utf8')

test('22.sql grants SYS_ADMIN the tenant operational menus', () => {
  assert.match(sql, /SYS_ADMIN/)
  for (const code of ['device', 'automation', 'alarm']) {
    assert.match(sql, new RegExp(`'${code}'`))
  }
  assert.match(sql, /UPDATE public\.sys_ui_elements/)
})
