#!/usr/bin/env node
// Immortality API client — agent CRUD interface for Articles.
// All writes go through https://battudao.com/api/articles/* with Bearer Firebase ID token.
// Mirror of khaitri.mjs — same auth/credential resolution, different collection path.

import { readFileSync, existsSync } from 'node:fs'
import { initializeApp } from 'firebase/app'
import { getAuth, signInWithEmailAndPassword, signOut } from 'firebase/auth'

loadEnvFile(process.env.IMMORTALITY_ENV_FILE || '/app/data/.env.immortality')

const API_BASE = process.env.IMMORTALITY_API_BASE || 'https://battudao.com/api'

const args = parseArgs(process.argv.slice(2))
const cmd = args.positional[0]

if (!cmd || cmd === 'help' || cmd === '--help') {
  printUsage()
  process.exit(cmd === 'help' || cmd === '--help' ? 0 : 2)
}

if (cmd === 'spec') {
  const spec = await fetchJson('GET', '/agent-spec')
  console.log(JSON.stringify(spec, null, 2))
  process.exit(0)
}

if (cmd === 'upload-image') {
  // Auth required for upload — but we can run without other commands' creds
  const creds = await resolveCredentials(args)
  const token = await signInGetIdToken(creds)
  const body = readStdinJson()
  // Body shape: { url, intent: "article"|"khaitri", slug }
  const out = await fetchJson('POST', '/upload-from-url', body, { Authorization: `Bearer ${token}` })
  console.log(JSON.stringify(out, null, 2))
  try { await signOut(getAuth()) } catch {}
  process.exit(out.ok ? 0 : 1)
}

// All other commands require auth
const creds = await resolveCredentials(args)
const token = await signInGetIdToken(creds)
const authHeader = { Authorization: `Bearer ${token}` }

let exitCode = 0
try {
  if (cmd === 'list') {
    const out = await fetchJson('GET', '/articles', null, authHeader)
    console.log(JSON.stringify(out, null, 2))
  } else if (cmd === 'get') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: get <id>'); exitCode = 2 }
    else {
      const out = await fetchJson('GET', `/articles/${encodeURIComponent(id)}`, null, authHeader)
      console.log(JSON.stringify(out, null, 2))
    }
  } else if (cmd === 'create') {
    const body = readStdinJson()
    const out = await fetchJson('POST', '/articles', body, authHeader)
    console.log(JSON.stringify(out, null, 2))
    if (!out.ok) exitCode = 1
  } else if (cmd === 'update') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: update <id>'); exitCode = 2 }
    else {
      const body = readStdinJson()
      const out = await fetchJson('PATCH', `/articles/${encodeURIComponent(id)}`, body, authHeader)
      console.log(JSON.stringify(out, null, 2))
      if (!out.ok) exitCode = 1
    }
  } else if (cmd === 'replace') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: replace <id>'); exitCode = 2 }
    else {
      const body = readStdinJson()
      const out = await fetchJson('PUT', `/articles/${encodeURIComponent(id)}`, body, authHeader)
      console.log(JSON.stringify(out, null, 2))
      if (!out.ok) exitCode = 1
    }
  } else if (cmd === 'delete') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: delete <id>'); exitCode = 2 }
    else {
      const out = await fetchJson('DELETE', `/articles/${encodeURIComponent(id)}`, null, authHeader)
      console.log(JSON.stringify(out, null, 2))
    }
  } else {
    console.error(`Unknown command: ${cmd}`)
    printUsage()
    exitCode = 2
  }
} finally {
  try { await signOut(getAuth()) } catch {}
}
process.exit(exitCode)

// ---------- helpers ----------

async function fetchJson(method, path, body, headers = {}) {
  const url = `${API_BASE}${path}`
  const init = { method, headers: { 'Content-Type': 'application/json', ...headers } }
  if (body != null) init.body = JSON.stringify(body)
  const res = await fetch(url, init)
  const text = await res.text()
  let json
  try { json = JSON.parse(text) } catch { json = { ok: false, error: 'invalid_json_response', raw: text.slice(0, 500) } }
  if (!res.ok && typeof json === 'object') json.http_status = res.status
  return json
}

async function signInGetIdToken({ email, password, apiKey, authDomain, projectId }) {
  const app = initializeApp({ apiKey, authDomain, projectId })
  const auth = getAuth(app)
  const cred = await signInWithEmailAndPassword(auth, email, password)
  return cred.user.getIdToken()
}

async function resolveCredentials(args) {
  if (args.flags.has('--stdin-creds')) {
    const data = readStdinJson()
    return {
      email: data.email,
      password: data.password,
      apiKey: data.apiKey || data['api-key'],
      authDomain: data.authDomain || data['auth-domain'],
      projectId: data.projectId || data['project-id'],
    }
  }
  const o = args.opts
  return {
    email: o['--email'] || process.env.IMMORTALITY_AGENT_EMAIL,
    password: o['--password'] || process.env.IMMORTALITY_AGENT_PASSWORD,
    apiKey: o['--api-key'] || process.env.IMMORTALITY_FIREBASE_API_KEY,
    authDomain: o['--auth-domain'] || process.env.IMMORTALITY_FIREBASE_AUTH_DOMAIN,
    projectId: o['--project-id'] || process.env.IMMORTALITY_FIREBASE_PROJECT_ID || 'immortalityvn',
  }
}

function readStdinJson() {
  const fileArg = process.argv.find(a => a.startsWith('--file='))
  if (fileArg) {
    const path = fileArg.slice('--file='.length)
    try {
      const buf = readFileSync(path, 'utf8')
      return JSON.parse(buf)
    } catch (e) {
      console.error(`--file=${path}: ${e.message}`)
      process.exit(2)
    }
  }
  let buf = ''
  try { buf = readFileSync(0, 'utf8') } catch { buf = '' }
  if (!buf.trim()) return {}
  try { return JSON.parse(buf) } catch (e) {
    console.error('stdin: invalid JSON —', e.message)
    process.exit(2)
  }
}

function loadEnvFile(path) {
  if (!path || !existsSync(path)) return
  try {
    const raw = readFileSync(path, 'utf8')
    for (const line of raw.split('\n')) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith('#')) continue
      const idx = trimmed.indexOf('=')
      if (idx < 1) continue
      const key = trimmed.slice(0, idx).trim()
      let val = trimmed.slice(idx + 1).trim()
      if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
        val = val.slice(1, -1)
      }
      if (!process.env[key]) process.env[key] = val
    }
  } catch {}
}

function parseArgs(argv) {
  const flags = new Set()
  const opts = {}
  const positional = []
  for (const a of argv) {
    if (a.startsWith('--') && a.includes('=')) {
      const [k, ...rest] = a.split('=')
      opts[k] = rest.join('=')
    } else if (a.startsWith('--')) {
      flags.add(a)
    } else {
      positional.push(a)
    }
  }
  return { flags, opts, positional }
}

function printUsage() {
  console.error(`articles.mjs <command> [args] [creds]

Commands (no auth):
  spec                            GET /api/agent-spec — fetch current schema + classification rules

Commands (auth required):
  list                            GET /api/articles
  get <id>                        GET /api/articles/:id
  create < data.json              POST /api/articles (409 if sourceRef exists)
  update <id> < patch.json        PATCH /api/articles/:id (partial)
  replace <id> < data.json        PUT /api/articles/:id (full)
  delete <id>                     DELETE /api/articles/:id
  upload-image < {url,intent,slug}  POST /api/upload-from-url — uploads source URL to R2,
                                  returns permanent R2 URL. Use BEFORE create or in PATCH flow.

Article schema (required):
  sourceRef, topic, date "YYYY-MM-DD", tag {vi,en}, vi {title, body}, en {title, body}, status:"draft"
Optional: image (R2 url from upload-image), summary {vi,en}, question {vi,en}

Credentials (in priority order):
  CLI args:    --email=... --password=... --api-key=... --auth-domain=... --project-id=...
  Stdin JSON:  --stdin-creds
  Env vars:    IMMORTALITY_AGENT_EMAIL, IMMORTALITY_AGENT_PASSWORD, IMMORTALITY_FIREBASE_API_KEY,
               IMMORTALITY_FIREBASE_AUTH_DOMAIN, IMMORTALITY_FIREBASE_PROJECT_ID

Examples:
  node articles.mjs spec
  node articles.mjs list
  echo '{"sourceRef":"x","topic":"tam-linh","date":"2026-05-06","tag":{...},"vi":{...},"en":{...}}' \\
    | node articles.mjs create
  echo '{"image":"https://pub-xxx.r2.dev/.../foo.jpg"}' | node articles.mjs update <id>
  echo '{"url":"https://api.telegram.org/.../foo.jpg","intent":"article","slug":"my-slug"}' \\
    | node articles.mjs upload-image
`)
}
