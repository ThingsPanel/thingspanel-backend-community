import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const fixture = readFileSync(join(root, 'test', 'fixtures', 'a100-demo-seed.sql'), 'utf8')
const migrationSql = readdirSync(join(root, 'sql'))
	.filter((name) => /^\d+\.sql$/.test(name))
	.map((name) => readFileSync(join(root, 'sql', name), 'utf8'))
	.join('\n')

test('A100 demo seed stays out of versioned migrations and resolves real dictionary IDs', () => {
  assert.doesNotMatch(migrationSql, /A100-demo|e0bd0c36-cc75-13cd-ed06-2b86d5b67e32/)
  assert.match(fixture, /A100-demo/)
  assert.match(fixture, /JOIN public\.sys_dict/)
  assert.doesNotMatch(fixture, /INSERT INTO public\.sys_dict_language\s*\([^)]*\)\s*VALUES/i)
})
