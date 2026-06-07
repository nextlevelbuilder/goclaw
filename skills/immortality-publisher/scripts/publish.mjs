#!/usr/bin/env node
// Immortality Publisher — Khai Tri inbox → immortality.vn Firestore.
// Contract: see ../SKILL.md and apps/immortality-vn/plans/260503-1302-immortality-publisher-spec/

import { readFile, writeFile, readdir, rename, mkdir } from 'node:fs/promises'
import { existsSync, readFileSync } from 'node:fs'
import { join, dirname, basename, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import matter from 'gray-matter'
import { initializeApp } from 'firebase/app'
import { getAuth, signInWithEmailAndPassword, signOut } from 'firebase/auth'
import {
  getFirestore, collection, addDoc, updateDoc, doc,
  query, where, getDocs, limit, serverTimestamp,
} from 'firebase/firestore'

const __dirname = dirname(fileURLToPath(import.meta.url))
const SKILL_DIR = resolve(__dirname, '..')
const DEFAULT_INBOX = resolve(SKILL_DIR, '..', '..', 'inbox', 'khaitri')
const SOURCE_TAG = 'goclaw-publisher-v1'

const cliArgs = parseArgs(process.argv.slice(2))
const DRY_RUN = cliArgs.flags.has('--dry-run')
const VERBOSE = cliArgs.flags.has('--verbose')
const STDIN_CREDS = cliArgs.flags.has('--stdin-creds')

// Credentials priority: CLI args → stdin JSON (--stdin-creds) → env vars (fallback for manual debug).
// Goclaw runtime should pass via CLI args or stdin so anh KHÔNG cần maintain .env per-app.
loadDotEnv(resolve(SKILL_DIR, '.env'))
const cfg = DRY_RUN ? {} : await resolveCredentials(cliArgs.opts)

const INBOX_DIR = cliArgs.opts['inbox'] || process.env.IMMORTALITY_INBOX_DIR || DEFAULT_INBOX
const DONE_DIR = join(INBOX_DIR, '_done')
const FAILED_DIR = join(INBOX_DIR, '_failed')
const LOG_DIR = join(INBOX_DIR, '_logs')

const logBuffer = []
const log = (...parts) => {
  const ts = new Date().toISOString().slice(0, 19).replace('T', ' ')
  const line = `[${ts}] ${parts.join(' ')}`
  logBuffer.push(line)
  console.log(line)
}
const logVerbose = (...parts) => { if (VERBOSE) log(...parts) }

main().catch(async err => {
  log('FATAL:', err.message || String(err))
  if (VERBOSE) console.error(err.stack)
  await flushLog().catch(() => {})
  process.exit(1)
})

async function main() {
  await ensureDir(DONE_DIR)
  await ensureDir(FAILED_DIR)
  await ensureDir(LOG_DIR)

  const files = await listInboxFiles()
  if (files.length === 0) {
    log('inbox empty — nothing to publish')
    return flushLog()
  }
  log(`found ${files.length} file(s) in ${INBOX_DIR}`)
  if (DRY_RUN) log('DRY-RUN mode — validate only, no Firebase init, no writes, no file moves')

  let auth = null, db = null
  if (!DRY_RUN) {
    const app = initializeApp({
      apiKey: cfg.apiKey,
      authDomain: cfg.authDomain,
      projectId: cfg.projectId,
    })
    auth = getAuth(app)
    db = getFirestore(app)
    await signInWithEmailAndPassword(auth, cfg.email, cfg.password)
    log(`auth: signed in as ${redactEmail(cfg.email)}`)
  }

  const stats = { added: 0, updated: 0, skipped: 0, failed: 0 }
  for (const file of files) {
    try {
      const outcome = await processFile(file, db)
      stats[outcome]++
    } catch (err) {
      stats.failed++
      log(`  ERROR: ${err.message}`)
      if (!DRY_RUN) await moveToFailed(file, err.message)
    }
  }

  if (!DRY_RUN) {
    await signOut(auth)
    log('auth: signed out')
  }
  log(`summary: ${stats.added} added · ${stats.updated} updated · ${stats.skipped} skipped · ${stats.failed} failed`)
  await flushLog()
}

async function listInboxFiles() {
  const entries = await readdir(INBOX_DIR, { withFileTypes: true })
  return entries
    .filter(e => e.isFile() && e.name.endsWith('.md') && !e.name.startsWith('_') && !e.name.startsWith('.'))
    .map(e => join(INBOX_DIR, e.name))
    .sort()
}

async function processFile(filePath, db) {
  const name = basename(filePath)
  log(`file: ${name}`)
  const raw = await readFile(filePath, 'utf-8')
  const { data: front, content } = matter(raw)
  const body = content.trim()
  validate(front, body)
  logVerbose(`  parse: ${Object.keys(front).length} frontmatter keys, ${body.length} body chars`)

  const docData = buildDoc(front, body)

  if (DRY_RUN) {
    log(`  dry-run: parse OK, would write (sourceRef=${docData.sourceRef}, order=${docData.order})`)
    return 'added'
  }

  const existing = await findBySourceRef(db, docData.sourceRef)

  if (existing) {
    const updates = buildUpdate(docData)
    await updateDoc(doc(db, 'khaitri', existing.id), updates)
    log(`  update: docId=${existing.id} (sourceRef exists, merged fields)`)
    await moveToDone(filePath)
    return 'updated'
  }

  const ref = await addDoc(collection(db, 'khaitri'), { ...docData, createdAt: serverTimestamp() })
  log(`  add: docId=${ref.id}`)
  await moveToDone(filePath)
  return 'added'
}

function validate(front, body) {
  const required = ['sourceRef', 'order', 'date', 'tagVi', 'titleVi', 'questionVi']
  for (const k of required) {
    if (front[k] === undefined || front[k] === null || front[k] === '') {
      throw new Error(`missing required frontmatter field: ${k}`)
    }
  }
  if (typeof front.sourceRef !== 'string') throw new Error('sourceRef must be a string')
  if (!Number.isInteger(front.order) || front.order < 1) throw new Error('order must be integer >= 1')
  if (!/^\d{4}-\d{2}-\d{2}$/.test(String(front.date))) throw new Error('date must match YYYY-MM-DD')
  if (front.status && front.status !== 'draft') {
    throw new Error(`status must be 'draft' or omitted (got '${front.status}'); only admin promotes to published`)
  }
  if (!body) throw new Error('body is empty')
  const hasViQA = /(^|\n)\s*Hỏi\s*[:：]/i.test(body) && /(^|\n)\s*Đáp\s*[:：]/i.test(body)
  const hasEnQA = /(^|\n)\s*Question\s*[:：]/i.test(body) && /(^|\n)\s*Answer\s*[:：]/i.test(body)
  if (!hasViQA && !hasEnQA) {
    throw new Error("body must contain at least one Hỏi:/Đáp: (or Question:/Answer:) pair")
  }
}

function splitBilingualBody(body) {
  // Split by top-level headings; classify each section by Hỏi: (vi) or Question:+Answer: (en).
  const sections = body.split(/(?=^# )/m).filter(s => s.trim())
  let vi = '', en = ''
  for (const s of sections) {
    const hasVi = /(^|\n)\s*Hỏi\s*[:：]/i.test(s)
    const hasEn = /(^|\n)\s*Question\s*[:：]/i.test(s) && /(^|\n)\s*Answer\s*[:：]/i.test(s)
    if (hasVi && !hasEn) vi += (vi ? '\n\n' : '') + s
    else if (hasEn) en += (en ? '\n\n' : '') + s
  }
  if (vi || en) return { vi: vi.trim(), en: en.trim() }
  // Fallback single-language detection (legacy single-body files)
  const isEn = /(^|\n)\s*Question\s*[:：]/i.test(body) && !/(^|\n)\s*Hỏi\s*[:：]/i.test(body)
  return isEn ? { vi: '', en: body } : { vi: body, en: '' }
}

function buildDoc(front, body) {
  const { vi: viBody, en: enBody } = splitBilingualBody(body)
  return {
    order: Number(front.order),
    date: String(front.date),
    tag: { vi: String(front.tagVi || ''), en: String(front.tagEn || '') },
    vi: {
      title: String(front.titleVi || ''),
      question: String(front.questionVi || ''),
      summary: String(front.summaryVi || ''),
      body: viBody,
    },
    en: {
      title: String(front.titleEn || ''),
      question: String(front.questionEn || ''),
      summary: String(front.summaryEn || ''),
      body: enBody,
    },
    sourceRef: String(front.sourceRef),
    source: String(front.source || SOURCE_TAG),
    status: 'draft',
  }
}

// Build dot-notation update payload — preserves admin's manual edits on fields
// that are empty in the incoming file (only writes non-empty optional fields).
function buildUpdate(d) {
  const updates = {
    order: d.order,
    date: d.date,
    'tag.vi': d.tag.vi,
    'vi.title': d.vi.title,
    'vi.question': d.vi.question,
    source: d.source,
    status: d.status,
    updatedAt: serverTimestamp(),
  }
  const optional = {
    'tag.en': d.tag.en,
    'vi.summary': d.vi.summary,
    'vi.body': d.vi.body,
    'en.title': d.en.title,
    'en.question': d.en.question,
    'en.summary': d.en.summary,
    'en.body': d.en.body,
  }
  for (const [k, v] of Object.entries(optional)) {
    if (v) updates[k] = v
  }
  return updates
}

async function findBySourceRef(db, sourceRef) {
  const q = query(collection(db, 'khaitri'), where('sourceRef', '==', sourceRef), limit(1))
  const snap = await getDocs(q)
  if (snap.empty) return null
  const d = snap.docs[0]
  return { id: d.id, ...d.data() }
}

async function moveToDone(filePath) {
  await rename(filePath, join(DONE_DIR, basename(filePath)))
}

async function moveToFailed(filePath, errMsg) {
  const name = basename(filePath)
  await rename(filePath, join(FAILED_DIR, name))
  await writeFile(join(FAILED_DIR, `${name}.error.txt`), errMsg + '\n', 'utf-8')
}

async function ensureDir(p) {
  if (!existsSync(p)) await mkdir(p, { recursive: true })
}

async function flushLog() {
  if (logBuffer.length === 0) return
  const ts = new Date().toISOString().replace(/[:.]/g, '').slice(0, 15) // YYYYMMDDTHHMMSS
  const file = join(LOG_DIR, `run-${ts}.log`)
  try {
    await writeFile(file, logBuffer.join('\n') + '\n', 'utf-8')
  } catch {
    // log dir not writable — already printed to console, continue
  }
}

function redactEmail(email) {
  const [user, domain] = String(email).split('@')
  if (!domain) return '***'
  return `${user.slice(0, 2)}***@${domain}`
}

// Parse CLI: supports `--flag` (boolean) and `--key=value` / `--key value` (string).
function parseArgs(argv) {
  const flags = new Set()
  const opts = {}
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (!a.startsWith('--')) continue
    const eq = a.indexOf('=')
    if (eq > 0) {
      opts[a.slice(2, eq)] = a.slice(eq + 1)
    } else if (i + 1 < argv.length && !argv[i + 1].startsWith('--')) {
      opts[a.slice(2)] = argv[++i]
    } else {
      flags.add(a)
    }
  }
  return { flags, opts }
}

// Resolve credentials in priority order: CLI args → stdin JSON → env vars.
async function resolveCredentials(opts) {
  let creds = {}
  if (STDIN_CREDS) {
    const stdinText = await readStdin()
    try {
      creds = JSON.parse(stdinText)
    } catch (e) {
      console.error('ERROR: --stdin-creds expects JSON on stdin:', e.message)
      process.exit(2)
    }
  }
  // CLI args override stdin; env fills any remaining gaps.
  const result = {
    email: opts['email'] || creds.email || process.env.IMMORTALITY_AGENT_EMAIL,
    password: opts['password'] || creds.password || process.env.IMMORTALITY_AGENT_PASSWORD,
    apiKey: opts['api-key'] || creds.apiKey || process.env.IMMORTALITY_FIREBASE_API_KEY,
    projectId: opts['project-id'] || creds.projectId || process.env.IMMORTALITY_FIREBASE_PROJECT_ID,
    authDomain: opts['auth-domain'] || creds.authDomain || process.env.IMMORTALITY_FIREBASE_AUTH_DOMAIN,
  }
  const missing = Object.entries(result).filter(([, v]) => !v).map(([k]) => k)
  if (missing.length) {
    console.error(`ERROR: missing credentials: ${missing.join(', ')}`)
    console.error('Provide via:')
    console.error('  CLI:    --email=X --password=Y --api-key=Z --project-id=W --auth-domain=V')
    console.error('  STDIN:  echo \'{"email":"X","password":"Y","apiKey":"Z","projectId":"W","authDomain":"V"}\' | publish.mjs --stdin-creds')
    console.error(`  ENV:    see ${resolve(SKILL_DIR, '.env.example')}`)
    process.exit(2)
  }
  return result
}

async function readStdin() {
  return new Promise((resolveP, reject) => {
    let buf = ''
    process.stdin.setEncoding('utf-8')
    process.stdin.on('data', chunk => { buf += chunk })
    process.stdin.on('end', () => resolveP(buf))
    process.stdin.on('error', reject)
  })
}

// Minimal .env loader — avoids dotenv dep. Skips lines starting with #, supports KEY=VAL (no quotes stripping).
function loadDotEnv(envPath) {
  if (!existsSync(envPath)) return
  const txt = readFileSync(envPath, 'utf-8')
  for (const line of txt.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const eq = trimmed.indexOf('=')
    if (eq < 0) continue
    const key = trimmed.slice(0, eq).trim()
    let val = trimmed.slice(eq + 1).trim()
    if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
      val = val.slice(1, -1)
    }
    if (!(key in process.env)) process.env[key] = val
  }
}
