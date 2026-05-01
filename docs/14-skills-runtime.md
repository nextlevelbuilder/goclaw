# 14 - Skills Runtime Environment

How skills access Python, Node.js, and system tools inside the Docker container. Covers image variants, pre-installed packages, runtime installation, and security constraints.

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│  Docker Container (Alpine 3.22, read_only: true)        │
│                                                         │
│  ┌─────────────────┐  ┌──────────────────────────────┐  │
│  │  Pre-installed   │  │  Writable Runtime Dir        │  │
│  │  (image layer)   │  │  /app/data/.runtime/         │  │
│  │                  │  │                              │  │
│  │  latest/alpine   │  │  pip/        ← PIP_TARGET   │  │
│  │  no py/node      │  │  pip-cache/  ← PIP_CACHE    │  │
│  │  python/node/full│  │  npm-global/ ← NPM_PREFIX   │  │
│  │  add runtimes    │  │                              │  │
│  └─────────────────┘  └──────────────────────────────┘  │
│                                                         │
│  Volumes (read-write):                                  │
│    /app/data      ← goclaw-data volume                  │
│    /app/workspace ← goclaw-workspace volume             │
│                                                         │
│  tmpfs (noexec):                                        │
│    /tmp           ← 256MB, no executables               │
└─────────────────────────────────────────────────────────┘
```

---

## 2. Pre-installed Packages (Option A)

Pre-installed runtimes depend on the Docker image variant you deploy. The Packages page and `/v1/packages/runtimes` report what exists inside the active GoClaw container, not what exists on the host machine.

### Runtime Variant Matrix

| Variant | Published tag | Build args | Pre-installed runtimes |
|---------|---------------|------------|------------------------|
| Minimal | `latest` | `ENABLE_PYTHON=false`, `ENABLE_NODE=false`, `ENABLE_FULL_SKILLS=false` | No Python or Node.js runtimes |
| Python | `python` | `ENABLE_PYTHON=true` | `python3`, `py3-pip`, `edge-tts` |
| Node | `node` | `ENABLE_NODE=true` | `nodejs`, `npm` |
| Full | `full` | `ENABLE_FULL_SKILLS=true` | `python3`, `py3-pip`, `nodejs`, `npm`, `pandoc`, `github-cli`, bundled skill deps |

### Full Variant Extras

#### Python Packages

| Package | Version | Used By |
|---------|---------|---------|
| `pypdf` | latest | pdf skill |
| `openpyxl` | latest | xlsx skill |
| `pandas` | latest | xlsx skill (data analysis) |
| `python-pptx` | latest | pptx skill |
| `markitdown` | latest | pptx skill (content extraction) |

#### Node.js Packages (global)

| Package | Used By |
|---------|---------|
| `docx` | docx skill (document creation) |
| `pptxgenjs` | pptx skill (presentation creation) |

---

## 3. Runtime Package Installation (Option B)

The entrypoint (`docker-entrypoint.sh`) configures writable directories so agents can install additional packages at runtime without `sudo`.

### Environment Variables (set by entrypoint)

```sh
# Python
PYTHONPATH=/app/data/.runtime/pip
PIP_TARGET=/app/data/.runtime/pip
PIP_BREAK_SYSTEM_PACKAGES=1
PIP_CACHE_DIR=/app/data/.runtime/pip-cache

# Node.js
NPM_CONFIG_PREFIX=/app/data/.runtime/npm-global
NODE_PATH=/usr/local/lib/node_modules:/app/data/.runtime/npm-global/lib/node_modules
PATH=/app/data/.runtime/npm-global/bin:/app/data/.runtime/pip/bin:$PATH
```

### How It Works

1. **Python**: `pip3 install <package>` installs to `/app/data/.runtime/pip/` (writable volume). `PYTHONPATH` ensures Python finds packages there.
2. **Node.js**: `npm install -g <package>` installs to `/app/data/.runtime/npm-global/`. `NODE_PATH` includes both system globals (`/usr/local/lib/node_modules`) and runtime globals.
3. **Persistence**: Packages installed at runtime persist across tool calls within the same container lifecycle (volume-backed).

### Agent Guidance

The system prompt and UI should treat runtime availability as variant-dependent:

```
Minimal `latest`: Python/Node may be missing in the container.
`python`, `node`, and `full` variants pre-install different runtimes.
To install additional packages: pip3 install <pkg> or npm install -g <pkg>
```

---

## 4. Security Constraints

| Constraint | Detail |
|------------|--------|
| `read_only: true` | Container rootfs is immutable; only volumes are writable |
| `/tmp` is `noexec` | Cannot execute binaries from tmpfs |
| `cap_drop: ALL` | No privilege escalation |
| `no-new-privileges` | Prevents setuid/setgid |
| Exec deny patterns | Blocks `curl \| sh`, reverse shells, crypto miners, etc. (see `shell.go`) |
| `.goclaw/` denied | Exec tool blocks access to `.goclaw/` except `.goclaw/skills-store/` |

### What Agents CAN Do

- Run Python/Node scripts via exec tool
- Install packages via `pip3 install` / `npm install -g`
- Access files in `/app/workspace/` including `.media/` subdirectory
- Read skill files from `.goclaw/skills-store/`

### What Agents CANNOT Do

- Write to system paths (rootfs is read-only)
- Execute binaries from `/tmp` (noexec)
- Access `.goclaw/` except skills-store
- Run denied shell patterns (network tools, reverse shells, etc.)

---

## 5. Media File Access

Uploaded files (from web chat, Telegram, Discord, etc.) are persisted to:

```
/app/workspace/.media/{sessionHash}/{uuid}.{ext}
```

The `enrichDocumentPaths()` function injects the full path into `<media:document>` tags:

```
<media:document name="report.pdf" path="/app/workspace/.media/abc123/uuid.pdf">
```

Agents can read these files directly via exec — no copy to `/tmp` needed.

---

## 6. Bundled Skills

Skills shipped with the Docker image at `/app/bundled-skills/`. Lowest priority in the loader hierarchy — user-uploaded skills (managed/skills-store) override them.

### Bundled Skills List

| Skill | Purpose |
|-------|---------|
| `pdf` | Read, create, merge, split PDFs |
| `xlsx` | Read, create, edit spreadsheets |
| `docx` | Read, create, edit Word documents |
| `pptx` | Read, create, edit presentations |
| `skill-creator` | Create new skills |

### How It Works

1. Skills source files live in `skills/` directory in the repo
2. Dockerfile copies them to `/app/bundled-skills/` in the image
3. `gateway.go` passes this path as `builtinSkills` to `skills.NewLoader()`
4. Loader priority: workspace > project-agents > personal-agents > global > **builtin** > managed

When a user uploads a skill with the same name via the UI, the managed version takes precedence.

### Adding a New Bundled Skill

1. Place skill directory under `skills/<name>/` with `SKILL.md` at root
2. Rebuild: `docker compose ... up -d --build`

---

## 7. Adding New Pre-installed Packages

To add a new package to the Docker image:

1. **Python**: Add to the `pip3 install` line in `Dockerfile` (usually `full`, sometimes `python`)
2. **Node.js**: Add to the `npm install -g` line in `Dockerfile` (usually `full`, sometimes `node`)
3. **System tool**: Add to the `apk add` line in `Dockerfile`
4. **Docs/UI guidance**: Update runtime variant docs and any UI copy that describes pre-installed tools
5. **Rebuild**: `docker compose ... up -d --build`

For packages only needed by specific skills, prefer runtime installation (Option B) to keep the image lean.

### GitHub Releases Installer

For CLI tools distributed as GitHub Releases (lazygit, starship, ripgrep, gh, etc.)
that aren't packaged via apk/pip/npm, use the `github:` runtime installer:

```
github:owner/repo[@tag]
```

Admin-only, SHA256-verified, ELF-validated, with a release-picker UI. Binaries
land in `/app/data/.runtime/bin/` (on `$PATH`). See
[`docs/packages-github.md`](./packages-github.md) for syntax, configuration,
security posture, and troubleshooting (especially musl/glibc compatibility).

---

## 8. Skill Search (v3)

Skills are searchable via BM25 keyword + semantic similarity matching (in `internal/skills/search.go`). The skill loader indexes all available skills from workspace/project/global/builtin sources. Skill discovery combines keyword matching with embeddings for improved recall of relevant tools to agent tasks.

---

## 9. Declaring Dependencies in SKILL.md

Auto-scan (`internal/skills/dep_scanner.go`) parses Python imports and npm requires from `scripts/` — adequate for most cases but has two limitations:

1. **Import name ≠ pip package name** for many packages (e.g. `import psycopg2` → must `pip install psycopg2-binary` because the sdist-only `psycopg2` package requires `pg_config` at build time). An import-to-pip alias table in `dep_checker.go` handles common cases (`psycopg2→psycopg2-binary`, `psycopg→psycopg[binary]`, `MySQLdb→mysqlclient`, `Crypto→pycryptodome`, `serial→pyserial`, `skimage→scikit-image`, `Levenshtein→python-Levenshtein`, plus the existing `cv2/PIL/yaml/sklearn/bs4/dateutil/dotenv/pptx/docx/attr/gi` set).
2. **False positives** — local helper modules detected as external deps.

Skill authors can override auto-scan with two optional frontmatter fields:

```yaml
---
name: my-skill
description: does things
deps:            # authoritative: when present, supersedes auto-scan for install
  - pip:psycopg2-binary
  - pip:requests>=2.31
  - pip:psycopg[binary]
  - npm:typescript
  - system:ffmpeg
  - github:cli/cli@v2.40.0
exclude_deps:    # filter false positives from auto-scan; ignored when deps: is set
  - pip:my_local_helper
---
```

**Prefix semantics:**

| Prefix | Effect | Example |
|--------|--------|---------|
| `pip:` | Python pip install | `pip:psycopg2-binary`, `pip:requests>=2.31` |
| `npm:` | Global npm install | `npm:typescript` |
| `github:` | GitHub Releases installer (admin) | `github:cli/cli@v2.40.0` |
| `system:` | apk package via pkg-helper | `system:ffmpeg` |
| (bare) | Treated as system binary | `pandoc` |

**Precedence:**

| `deps:` | `exclude_deps:` | Behavior |
|---------|-----------------|----------|
| absent  | absent | Auto-scan as today |
| absent  | present | Auto-scan minus `exclude_deps` entries |
| present | — | Explicit deps used (authoritative); auto-scan kept only for advisory log |

**v1 limitations:**

- Version pins in `pip:requests>=2.31` are stripped when checking whether the import is available (checker imports `requests`); the installer currently installs latest. Full pin pass-through is planned for v2.
- `deps:` bypasses the import-to-pip alias map, so authors must declare the exact pip package name (e.g. `pip:psycopg2-binary`, not `pip:psycopg2`).
- Unknown prefixes in `deps:` are treated as system binaries.
- `exclude_deps` matches surface in `slog.Debug` only; no UI diagnostic yet.

**Validation & safety:**

Manifest dep strings are passed to `python3 -c` / `node -e` at check time, so each entry is validated against a per-category allowlist before use:

| Category | Allowed chars | Example reject |
|----------|---------------|----------------|
| `pip:` | `[A-Za-z_][A-Za-z0-9_.-]*` | `pip:foo;__import__('os')...` |
| `npm:` | `^(@scope/)?[a-z0-9][a-z0-9_.-]*` | `npm:a');require(...` |
| `system:` / bare | `[A-Za-z0-9][A-Za-z0-9._+-]*` | `rm -rf /`, `$(evil)` |

Invalid entries are dropped with `slog.Warn("skills: dropping invalid manifest dep", ...)`. Malformed specs like `pip:>=1.0` (no package name) or `pip:[binary]` (extras only) are also dropped.

**YAML grammar subset accepted by the loader:**

- Flat list only: `deps:\n  - item1\n  - item2`
- Quoted items OK (`"..."` or `'...'`)
- CRLF normalized
- Flow-style `[a, b]` NOT supported (returns empty)
- Dash without space `-item` NOT supported
- Nested maps dropped with warning (avoids silent prefix-loss miscategorization)
