# 14 - Skills Runtime Environment

How skills access Python, Node.js, and system tools inside Docker containers and bare-metal gateway deployments. Covers image variants, pre-installed packages, runtime installation, and security constraints.

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

Explicit skill activation is handled before runtime execution. When a user starts
their prompt with `/<skill-slug>` or `/use <skill name>`, the gateway resolves
the skill, injects its `SKILL.md` into the current turn, and then normal runtime
rules apply to any scripts or package dependencies that skill uses.

---

## 2. Pre-installed Packages (Option A)

Pre-installed runtimes depend on the Docker image variant you deploy. The Packages page and `/v1/packages/runtimes` report what exists inside the active GoClaw container, not what exists on the host machine.

### Runtime Variant Matrix

| Variant | Published tag | Build args | Pre-installed runtimes |
|---------|---------------|------------|------------------------|
| Latest | `latest` | `ENABLE_PYTHON=true`, `ENABLE_NODE=false`, `ENABLE_FULL_SKILLS=false` | `python3`, `py3-pip`, shared Python deps |
| Base | `base` | `ENABLE_PYTHON=false`, `ENABLE_NODE=false`, `ENABLE_FULL_SKILLS=false` | No Python or Node.js runtimes |
| Full | `full` | `ENABLE_FULL_SKILLS=true` | `python3`, `py3-pip`, `nodejs`, `npm`, `pandoc`, `github-cli`, bundled skill deps, Workspace CLI |
| Custom Python | not published | `ENABLE_PYTHON=true` | `python3`, `py3-pip`, shared Python deps |
| Custom Node | not published | `ENABLE_NODE=true` | `nodejs`, `npm` |

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
| `@googleworkspace/cli` (`gws`) | Google Workspace CLI for Drive, Gmail, Calendar, and Workspace APIs |

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

### Bare-Metal Ubuntu/Debian

When the gateway runs directly on Ubuntu/Debian instead of inside the Alpine Docker image:

1. `pip:<name>` still runs `pip3 install --break-system-packages <name>`.
2. `npm:<name>` runs `npm install -g <name>` with a GoClaw-owned prefix at `{runtimeDir}/npm-global` instead of `/usr/lib/node_modules`.
3. Bare system package names use `sudo -n apt-get install -y --no-install-recommends <name>`.
4. Compatibility aliases: `pip3` installs `python3-pip`; `github-cli` installs `gh`.
5. Installed apt packages are recorded in `{runtimeDir}/system-packages.json` so the System Packages table can show the user-facing name (`github-cli`) while checking the real apt package (`gh`).
6. `/tmp/pkg.sock` is Docker/Alpine-only and is not required on bare-metal Ubuntu/Debian.

Default `{runtimeDir}` resolution:

1. `RUNTIME_DIR`, when set.
2. `GOCLAW_DATA_DIR/.runtime`, when `GOCLAW_DATA_DIR` is set.
3. `/var/lib/goclaw/data/.runtime` on bare-metal Linux.
4. `/app/data/.runtime` in Docker-style runtime.

### Agent Guidance

The system prompt and UI should treat runtime availability as variant-dependent:

```
Published `latest`: Python is present; Node may be missing in the container.
Published `full`: Python, Node, and full skill extras are present.
Published `base`: Python and Node are absent.
Custom builds can set ENABLE_PYTHON=true or ENABLE_NODE=true.
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
- Access files in `/app/workspace/`, including `.uploads/` for current user uploads and `.media/` for legacy media refs
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
/app/workspace/.uploads/{safe-original-name}-{8hex}.{ext}
```

Uploads without a usable original filename fall back to `{uuid}.{ext}`. Legacy media refs may still resolve from `.media/{sessionHash}/{uuid}.{ext}`.

The `enrichDocumentPaths()` function injects the full path into `<media:document>` tags:

```
<media:document name="report.pdf" path="/app/workspace/.uploads/report-a1b2c3d4.pdf">
```

Agents can read these files directly via exec — no copy to `/tmp` needed. For archive uploads such as `.zip`, inspect or extract with commands like `unzip -l "<path>"` or `unzip -q "<path>" -d <output-dir>`.

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
| `workspace-organizing` | Organize shared workspaces and generated files |
| `goclaw` | Operate and debug GoClaw gateway CLI/runtime administration |

### How It Works

1. Skills source files live in `skills/` directory in the repo
2. Dockerfile copies them to `/app/bundled-skills/` in the image
3. `gateway.go` passes this path as `builtinSkills` to `skills.NewLoader()`
4. Loader priority: workspace > project-agents > personal-agents > global > managed > **builtin**

When a user uploads a skill with the same name via the UI, the managed version takes precedence.

### Adding a New Bundled Skill

1. Place skill directory under `skills/<name>/` with `SKILL.md` at root
2. Rebuild: `docker compose ... up -d --build`

---

## 7. Adding New Pre-installed Packages

To add a new package to the Docker image:

1. **Python**: Add to the `pip3 install` line in `Dockerfile` (usually `full`, sometimes `python`)
2. **Node.js**: Add to the `npm install -g` line in `Dockerfile` (usually `full`, sometimes a custom `ENABLE_NODE=true` build)
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
land in `{runtimeDir}/bin/` (on `$PATH`). See
[`docs/packages-github.md`](./packages-github.md) for syntax, configuration,
security posture, and troubleshooting (especially musl/glibc compatibility).

### Update Flow (Phase 1: GitHub only)

GitHub binaries support proactive update checking via:

- UI summary bar on the Runtime & Packages page (badge + Refresh + Update All)
- `/v1/packages/updates*` endpoints (master-scope for writes)
- Atomic two-phase `.bak` swap with automatic rollback
- ETag-aware polling (304 = zero rate-limit cost)
- Pre-release handling via regex + `release.prerelease` + semver ordering

See [`docs/packages-github.md`](./packages-github.md) § "Updating Installed
Packages" for the full contract, troubleshooting, and runbook.

Pip/npm/apk update flows are **deferred to Phase 2** — the `UpdateChecker` /
`UpdateExecutor` interfaces in `internal/skills/update_registry.go` are
designed for interface-based extension without Phase 1 refactor.

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
| `npm:` | Global npm install under GoClaw runtime prefix | `npm:typescript`, `npm:@aiagentwiki/cli` |
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
