#!/usr/bin/env node
// Immortality API client — agent CRUD interface for Khai Trí.
// All writes go through https://battudao.com/api/khaitri/* with Bearer Firebase ID token.

import { readFileSync, existsSync } from 'node:fs'
import { initializeApp } from 'firebase/app'
import { getAuth, signInWithEmailAndPassword, signOut } from 'firebase/auth'

// Auto-load central env file if present. User sets up once at /app/data/.env.immortality;
// agent never has to handle credentials directly. Order: explicit --env-file → central file → process.env.
loadEnvFile(process.env.IMMORTALITY_ENV_FILE || '/app/data/.env.immortality')

const API_BASE = process.env.IMMORTALITY_API_BASE || 'https://battudao.com/api'

const args = parseArgs(process.argv.slice(2))
const cmd = args.positional[0]

if (!cmd || cmd === 'help' || cmd === '--help') {
  printUsage()
  process.exit(cmd === 'help' || cmd === '--help' ? 0 : 2)
}

if (cmd === 'spec') {
  // No auth needed
  const spec = await fetchJson('GET', '/agent-spec')
  console.log(JSON.stringify(spec, null, 2))
  process.exit(0)
}

if (cmd === 'validate') {
  // No auth needed (dry-run)
  const body = readStdinJson()
  const mode = args.opts['--mode'] || 'create'
  const out = await fetchJson('POST', '/khaitri/validate', { data: body, mode })
  console.log(JSON.stringify(out, null, 2))
  process.exit(out.ok ? 0 : 1)
}

// All other commands require auth
const creds = await resolveCredentials(args)
const token = await signInGetIdToken(creds)
const authHeader = { Authorization: `Bearer ${token}` }

let exitCode = 0
try {
  if (cmd === 'list') {
    const out = await fetchJson('GET', '/khaitri', null, authHeader)
    console.log(JSON.stringify(out, null, 2))
  } else if (cmd === 'get') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: get <id>'); exitCode = 2 }
    else {
      const out = await fetchJson('GET', `/khaitri/${encodeURIComponent(id)}`, null, authHeader)
      console.log(JSON.stringify(out, null, 2))
    }
  } else if (cmd === 'create') {
    const body = readStdinJson()
    const out = await fetchJson('POST', '/khaitri', body, authHeader)
    console.log(JSON.stringify(out, null, 2))
    if (!out.ok) exitCode = 1
  } else if (cmd === 'update') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: update <id>'); exitCode = 2 }
    else {
      const body = readStdinJson()
      const out = await fetchJson('PATCH', `/khaitri/${encodeURIComponent(id)}`, body, authHeader)
      console.log(JSON.stringify(out, null, 2))
      if (!out.ok) exitCode = 1
    }
  } else if (cmd === 'replace') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: replace <id>'); exitCode = 2 }
    else {
      const body = readStdinJson()
      const out = await fetchJson('PUT', `/khaitri/${encodeURIComponent(id)}`, body, authHeader)
      console.log(JSON.stringify(out, null, 2))
      if (!out.ok) exitCode = 1
    }
  } else if (cmd === 'delete') {
    const id = args.positional[1]
    if (!id) { console.error('Usage: delete <id>'); exitCode = 2 }
    else {
      const out = await fetchJson('DELETE', `/khaitri/${encodeURIComponent(id)}`, null, authHeader)
      console.log(JSON.stringify(out, null, 2))
    }
  } else {
    console.error(`Unknown command: ${cmd}`)
    printUsage()
    exitCode = 2
  }
} finally {
  // signOut quietly — token already passed to API, nothing client-side to clean
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
  // Priority: --file=path → stdin. --file is preferred in goclaw exec sandbox
  // because Direct Exec mode does not support shell pipes.
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
  console.error(`khaitri.mjs <command> [args] [creds]

Commands (no auth):
  spec                            GET /api/agent-spec — fetch current schema + rules
  validate < data.json            POST /api/khaitri/validate — dry-run

Commands (auth required):
  list                            GET /api/khaitri
  get <id>                        GET /api/khaitri/:id
  create < data.json              POST /api/khaitri (409 if sourceRef exists)
  update <id> < patch.json        PATCH /api/khaitri/:id (partial)
  replace <id> < data.json        PUT /api/khaitri/:id (full)
  delete <id>                     DELETE /api/khaitri/:id

Credentials (in priority order):
  CLI args:    --email=... --password=... --api-key=... --auth-domain=... --project-id=...
  Stdin JSON:  --stdin-creds  (combine with command stdin via process substitution)
  Env vars:    IMMORTALITY_AGENT_EMAIL, IMMORTALITY_AGENT_PASSWORD, IMMORTALITY_FIREBASE_API_KEY, ...

Examples:
  node khaitri.mjs spec
  node khaitri.mjs list --email=X --password=Y --api-key=Z --auth-domain=W --project-id=V
  echo '{"sourceRef":"x","order":7,"date":"2026-05-04",...}' | node khaitri.mjs validate
`)
}
