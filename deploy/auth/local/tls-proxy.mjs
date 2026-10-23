// Local acceptance only. Run with a directory containing cert.pem/key.pem.
// Binds loopback; does not change the production origin or TLS verification.
import https from 'node:https'
import http from 'node:http'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
const dir = process.argv[2]
if (!dir) throw new Error('Usage: node tls-proxy.mjs <private-fixture-directory>')
const tls = { cert: readFileSync(join(dir, 'cert.pem')), key: readFileSync(join(dir, 'key.pem')) }
function proxy(req, res, port, path = req.url) {
  const upstream = http.request({ host: '127.0.0.1', port, path, method: req.method, headers: { ...req.headers, 'x-forwarded-proto': 'https' } }, response => {
    res.writeHead(response.statusCode, response.headers); response.pipe(res)
  })
  upstream.on('error', () => { res.writeHead(503); res.end('Local fixture unavailable') })
  req.pipe(upstream)
}
https.createServer(tls, (req, res) => proxy(req, res, 24442)).listen(24443, '127.0.0.1')
https.createServer(tls, (req, res) => {
  const path = req.url.replace(/^\/proxy-default(?=\/)/, '/api/v1')
  // Profile display uses the isolated account detail; profile editing and device
  // business APIs are deliberately outside this authentication acceptance rig.
  if (path === '/api/v1/board/user/info') return proxy(req, res, 24445, '/api/v1/user/detail')
  if (/^\/api\/v1\/(auth\/|login(?:\?|$)|user\/(detail|refresh|logout)(?:\?|$)|tenant\/(setup-state|has-admin)(?:\?|$))/.test(path)) return proxy(req, res, 24445, path)
  // Non-auth public branding/function flags use the existing local community
  // backend. Auth acceptance does not assert unrelated device business pages.
  if (/^\/api\/v1\/(logo|sys_function|sys_version)(\?|$)/.test(path)) return proxy(req, res, 9999, path)
  if (path.startsWith('/api/v1/')) { res.writeHead(503, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ code: 503, message: 'Outside isolated authentication fixture', data: null })); return }
  proxy(req, res, 5003)
}).listen(24444, '127.0.0.1')
console.log('Local identity provider: https://localhost:24443; ThingsPanel: https://localhost:24444')
