#!/usr/bin/env node
// Immortality Editor — fetch a Firestore Khai Trí doc by sourceRef back into a markdown file.
// Output shape matches what immortality-publisher expects, so the standard publish workflow
// can re-apply the edited markdown as an UPDATE (sourceRef-based upsert).

import { writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { initializeApp } from 'firebase/app'
import { getAuth, signInWithEmailAndPassword, signOut } from 'firebase/auth'
import { getFirestore, collection, query, where, getDocs, limit } from 'firebase/firestore'

const args = parseArgs(process.argv.slice(2))
const sourceRef = args.opts['--sourceRef'] || args.opts['--source-ref']
const outputPath = args.opts['--output'] || args.opts['--out']

if (!sourceRef) {
  console.error('Usage: fetch.mjs --sourceRef <ref> --output <path> [creds...]')
  console.error('Required creds: --api-key --project-id --auth-domain --email --password')
  process.exit(2)
}
if (!outputPath) {
  console.error('Missing --output <path>')
  process.exit(2)
}

const cfg = {
  apiKey: args.opts['--api-key'] || process.env.IMMORTALITY_FIREBASE_API_KEY,
  projectId: args.opts['--project-id'] || process.env.IMMORTALITY_FIREBASE_PROJECT_ID,
  authDomain: args.opts['--auth-domain'] || process.env.IMMORTALITY_FIREBASE_AUTH_DOMAIN,
  email: args.opts['--email'] || process.env.IMMORTALITY_AGENT_EMAIL,
  password: args.opts['--password'] || process.env.IMMORTALITY_AGENT_PASSWORD,
}
for (const [k, v] of Object.entries(cfg)) {
  if (!v) { console.error(`Missing credential: ${k}`); process.exit(2) }
}

const app = initializeApp({ apiKey: cfg.apiKey, projectId: cfg.projectId, authDomain: cfg.authDomain })
const auth = getAuth(app)
await signInWithEmailAndPassword(auth, cfg.email, cfg.password)
const db = getFirestore(app)

const q = query(collection(db, 'khaitri'), where('sourceRef', '==', sourceRef), limit(1))
const snap = await getDocs(q)
if (snap.empty) {
  console.error(`No doc with sourceRef=${sourceRef}`)
  await signOut(auth)
  process.exit(1)
}
const doc = snap.docs[0]
const data = doc.data()
const md = renderMarkdown(data)
await writeFile(resolve(outputPath), md, 'utf8')
await signOut(auth)
console.log(`fetched: ${doc.id} → ${outputPath} (${md.length} chars)`)
console.log(`hasVi: ${!!data.vi?.title}, hasEn: ${!!data.en?.title}, order: ${data.order}`)

function renderMarkdown(d) {
  const fm = {
    sourceRef: d.sourceRef,
    order: d.order,
    date: `'${d.date}'`,
    tagVi: d.tag?.vi || '',
    tagEn: d.tag?.en || '',
    titleVi: d.vi?.title || '',
    titleEn: d.en?.title || '',
    questionVi: d.vi?.question || '',
    questionEn: d.en?.question || '',
    summaryVi: d.vi?.summary || '',
    summaryEn: d.en?.summary || '',
  }
  const fmYaml = Object.entries(fm).map(([k, v]) => `${k}: ${formatYamlValue(v)}`).join('\n')
  const viBody = d.vi?.body || ''
  const enBody = d.en?.body || ''
  const body = [viBody, enBody].filter(Boolean).join('\n\n---\n\n')
  return `---\n${fmYaml}\n---\n\n${body}\n`
}

function formatYamlValue(v) {
  if (v === '' || v == null) return "''"
  const s = String(v)
  // already-quoted (date) or pure number → emit raw
  if (/^'.*'$/.test(s)) return s
  if (/^-?\d+(\.\d+)?$/.test(s) && !s.startsWith('0')) return s
  // contains special YAML chars → quote
  if (/[:#&*!|>'"%@`]|^\s|\s$/.test(s)) return JSON.stringify(s)
  return s
}

function parseArgs(argv) {
  const flags = new Set()
  const opts = {}
  for (const a of argv) {
    if (a.includes('=')) {
      const [k, ...rest] = a.split('=')
      opts[k] = rest.join('=')
    } else if (a.startsWith('--')) {
      flags.add(a)
    }
  }
  return { flags, opts }
}
