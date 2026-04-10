# 22 - Skill Hub: Installation & Package Management

User-driven skill discovery, installation, and lifecycle management via CLI and future HTTP API. Installs from public skill registry (curated GitHub repositories) or direct GitHub URLs with dependency validation and hot-reload.

---

## 1. Overview

The Skill Hub extends the skills system with **end-user skill discovery and installation**:

| Layer | Component | Purpose |
|-------|-----------|---------|
| **CLI** | `goclaw skills install`, `remove`, `search` | Human-friendly skill lifecycle |
| **Registry** | `registry_client.go` | JSON-based skill index on GitHub |
| **Fetcher** | `github_fetcher.go` | Tarball download + secure extraction |
| **Installer** | `installer.go` | Validation, copy, DB registration, hot-reload |

Skills are stored in **versioned directories** under `skills-store/{slug}/{version}/`. Each installation increments the version, enabling safe re-installations and rollback potential.

**Security perimeter:**
- Package name validation (stdlib blocklist)
- SKILL.md content scanning (GuardSkillContent)
- Tar bomb prevention (50 MB download, 20 MB extracted, 500 files)
- Path traversal hardening in extraction

---

## 2. CLI Commands

### 2.1 Install from Registry

Registry lookup via slug (curated GitHub repos):

```bash
goclaw skills install shopee-product-finder
```

Resolves `shopee-product-finder` slug → GitHub URL via `registry-index.json`, then fetches and installs. Optional ref override:

```bash
goclaw skills install shopee-product-finder --ref v2.0
```

### 2.2 Install from GitHub

Direct GitHub repo (owner/repo format or full URL):

```bash
goclaw skills install owner/repo
goclaw skills install github.com/owner/repo
goclaw skills install owner/repo@v1.0
goclaw skills install github.com/owner/repo --ref main
```

Formats accepted:
- `owner/repo` → GitHub shorthand, default branch
- `owner/repo@v1.0` → Shorthand with specific ref
- `github.com/owner/repo` → Full URL, default branch
- `github.com/owner/repo@v1.0` → Full URL with ref

### 2.3 Remove

```bash
goclaw skills remove my-skill-slug
```

Deletes the skill from `skills-store/` and marks as archived in the database.

### 2.4 Search Registry

```bash
goclaw skills search pdf
goclaw skills search "text processing"
```

Queries registry index for matching slug/description/tags. Returns:
- Slug (install name)
- Repo (GitHub location)
- Description
- Tags

### 2.5 List & Show

```bash
goclaw skills list                    # All skills (system + custom)
goclaw skills show my-skill           # Full SKILL.md content + metadata
goclaw skills show my-skill --json    # JSON metadata
```

---

## 3. Registry Architecture

### 3.1 Registry Index Format

Central registry at `https://raw.githubusercontent.com/goclaw-hub/registry/main/index.json`:

```json
{
  "skills": [
    {
      "slug": "shopee-product-finder",
      "repo": "owner/shopee-product-finder",
      "description": "Search Shopee for products",
      "tags": ["shopping", "e-commerce", "shopee"]
    },
    {
      "slug": "web-scraper",
      "repo": "owner/web-scraper",
      "description": "Scrape and extract web content",
      "tags": ["web", "scraping", "data"]
    }
  ]
}
```

### 3.2 Registry Client (registry_client.go)

Fetches and caches the index locally:

```go
type RegistryClient struct {
    cacheDir string
    indexURL string       // GOCLAW_REGISTRY_URL env or default
    cacheTTL time.Duration
}

// Resolve("shopee-product-finder") → ("owner", "shopee-product-finder")
Resolve(ctx, slug) (owner, repo, error)
```

**Cache strategy:**
- Stores `registry-index.json` in `{dataDir}/cache/` (1 MB max)
- 1-hour TTL; expires after first fetch past TTL
- HTTPS-only (MITM protection; non-HTTPS URLs rejected)
- Env override: `GOCLAW_REGISTRY_URL`

---

## 4. Fetcher & Extraction (github_fetcher.go)

### 4.1 Tarball Download

Fetches GitHub repository as `.tar.gz` via tarball API:

```
GET https://api.github.com/repos/{owner}/{repo}/tarball/{ref}
  → 302 redirect to S3 (temporary signed URL)
  → Download to temp file (streaming)
```

**Size limits:**
- Max download: 50 MB (prevents resource exhaustion)
- Max extracted: 20 MB (prevents decompression bombs)
- Max files: 500 (tar bomb prevention)
- Timeout: 30 seconds

### 4.2 Secure Extraction

Hardened tar extraction in `internal/skills/github_fetcher.go`:

1. **Reject path traversal:** Skip entries containing `..`
2. **Skip symlinks:** Prevent escape via symbolic links
3. **Skip system artifacts:** `.DS_Store`, `__MACOSX`, `Thumbs.db`, `node_modules/`
4. **Track file count:** Reject tarballs with 500+ files
5. **Track extracted size:** Reject if >20 MB total
6. **Extract to temp:** Uses OS temp dir, caller responsible for cleanup

### 4.3 SkillRef Parser

Parses user input into structured reference:

```go
type SkillRef struct {
    Owner      string // GitHub owner
    Repo       string // Repository name
    Ref        string // Tag, branch, or "" (default)
    IsRegistry bool   // true if input was plain slug
}

ParseSkillRef("shopee-product-finder")    // {IsRegistry: true}
ParseSkillRef("owner/repo")                // {Owner: "owner", Repo: "repo"}
ParseSkillRef("owner/repo@v1.0")           // {Owner: "owner", Repo: "repo", Ref: "v1.0"}
```

---

## 5. Installation Flow (installer.go)

SkillInstaller orchestrates multi-stage skill installation into database and filesystem:

```
User calls: goclaw skills install owner/repo
    │
    ▼
1. Fetch tarball via github_fetcher → extract to temp dir
    │
    ▼
2. SkillInstaller.Install(ctx, srcDir, ownerID)
    │
    ├─ Read SKILL.md from srcDir
    ├─ Guard scan (GuardSkillContent)
    ├─ Parse frontmatter (name, slug, description)
    ├─ Reject system skill conflict (pdf, docx, skill-creator, etc.)
    ├─ Compute SHA-256 hash
    ├─ Check dir size (max 20 MB)
    ├─ GetNextVersionLocked(slug) → version N
    ├─ Copy srcDir → skills-store/{slug}/{N}/
    ├─ INSERT/UPSERT skills table
    ├─ ScanSkillDeps + CheckSkillDeps → warn on missing
    ├─ BumpVersion() → invalidate loader cache
    ├─ Return SkillInstallResult {ID, Name, Slug, Version, DepsWarning}
    │
    ▼
3. Caller cleans up srcDir (temp directory)
    │
    ▼
Success: Skill hot-reloaded, usable by agents
```

### 5.1 SkillInstallResult

```go
type SkillInstallResult struct {
    ID          uuid.UUID  `json:"id"`              // Skill UUID in DB
    Name        string     `json:"name"`            // Display name
    Slug        string     `json:"slug"`            // Kebab-case identifier
    Version     int        `json:"version"`         // Version number
    DepsWarning string     `json:"deps_warning,omitempty"` // Missing deps (if any)
}
```

### 5.2 Concurrent Safety

Uses advisory lock (`GetNextVersionLocked`) to prevent race conditions during:
1. Version calculation
2. Directory copy
3. DB insert

Lock released after operation (deferred cleanup).

---

## 6. Security Model

### 6.1 Package Name Validation

`validatePackageName()` in `dep_installer.go` blocks stdlib packages:

```go
// Reject these in Python dependency scanning:
"sys", "os", "subprocess", "socket", "urllib", "requests", "cryptography", ...
```

Rationale: Prevents agents from accidentally installing stdlib aliases or dangerous packages.

### 6.2 SKILL.md Content Guard

`GuardSkillContent()` scans for dangerous patterns:

| Pattern | Reason | Mitigation |
|---------|--------|-----------|
| `eval`, `exec`, `__import__` | Dynamic code exec | Detection + warning |
| `open(`, `os.system` | Unrestricted file/shell access | Detection + warning |
| Base64-encoded payloads | Obfuscation | Detection + warning |
| Raw `.pyc` / compiled code | Binary obfuscation | Detection + warning |

**Mode:** Detection-only (returns violations, allows install but warns agent).

### 6.3 Tar Extraction Hardening

```go
// Per-entry checks during tar extraction:
if strings.Contains(header.Name, "..") {
    skip  // Path traversal
}
if header.Typeflag == tar.TypeSymlink {
    skip  // Symlink escape
}
if isSystemArtifact(header.Name) {
    skip  // .DS_Store, __MACOSX, node_modules, etc.
}
// Track extracted file count (reject if >500)
// Track extracted size (reject if >20 MB)
```

### 6.4 GitHub API Rate Limiting

Registry client respects GitHub's unauthenticated rate limit (60 req/hour). No auth token required; cache mitigates repeated fetches.

---

## 7. Dependency Validation

Post-installation dependency scanning mirrors `publish_skill` tool:

```
After Install:
    │
    ├─ ScanSkillDeps(skillDir)
    │   └─ Detect binaries, Python (pip), Node (npm) in scripts/
    │
    ├─ CheckSkillDeps(manifest)
    │   └─ Verify each dependency available on system
    │
    └─ On missing deps:
        ├─ Store in skills.deps JSONB column
        ├─ Return DepsWarning in SkillInstallResult
        └─ Do NOT archive (unlike HTTP upload handler)
```

Users are informed of missing deps but can still use the skill if they manage dependencies manually.

---

## 8. Hot-Reload Integration

After successful installation:

```go
si.loader.BumpVersion()  // Invalidates in-memory cache version
```

Next agent access to skill loads from `skills-store/{slug}/{version}/` directory automatically.

---

## 9. File Organization

```
internal/skills/
├── github_fetcher.go       # Tarball download + secure extraction
├── registry_client.go      # Registry index fetch + cache
├── installer.go            # Multi-stage installation orchestrator
├── guard.go                # SKILL.md content scanning
├── dep_installer.go        # validatePackageName() + dep checks
└── dep_checker.go          # Dependency analysis
```

```
cmd/
├── skills_cmd.go           # Main `skills` command group
├── skill_install_cmd.go    # `skills install` subcommand
└── skill_remove_cmd.go     # `skills remove` subcommand
```

---

## 10. Future Extensions

- **HTTP API:** POST `/v1/skills/install`, DELETE `/v1/skills/{slug}` for Web UI
- **Agent tool:** `install_skill(slug|url, ref?)` builtin for agent-driven installation
- **Rollback:** Version snapshots + downgrade support
- **Signing:** GPG-signed skill registry entries (integrity verification)
- **Private registry:** Custom registry URL support (already in place via env var)

---

## 11. Example Workflow

**Scenario: User discovers skill via `goclaw skills search`**

```bash
$ goclaw skills search shopee
[1] shopee-product-finder (owner/shopee-product-finder)
    Search Shopee for products
    Tags: shopping, e-commerce, shopee

$ goclaw skills install shopee-product-finder
Resolving "shopee-product-finder" from registry...
Resolved to: owner/shopee-product-finder
Fetching owner/shopee-product-finder (default branch)...
Downloaded 5.2 MB tarball
Extracting...
Validating SKILL.md...
Installing to skills-store/shopee-product-finder/1/
Skill installed: ID=<uuid>, v1.0
Checking dependencies...
Warning: requires Python requests library (pip install requests)
```

**Skill now usable in agents immediately.**

---

## 12. Error Handling

| Error | HTTP Status | Cause | Action |
|-------|------------|-------|--------|
| Slug conflict with system skill | 400 | `IsSystemSkill()` check | Suggest different slug |
| Dir too large (>20 MB) | 413 | Size check in extractor | Reduce skill complexity |
| SKILL.md missing/empty | 400 | Read/parse fail | Verify repo structure |
| Invalid slug format | 400 | Slug validation | Suggest auto-derived slug |
| Download timeout (30s) | 504 | GitHub API slow | Retry with smaller repo |
| Tar bomb (>500 files) | 413 | File count exceeded | Simplify repo structure |
| Path traversal in tar | 400 | Extraction guard | Report malicious tar |
| Missing SKILL.md frontmatter | 400 | name field absent | Verify skill manifest |

---

## 13. Dependencies

- `archive/tar`, `compress/gzip` — Tar extraction
- `crypto/sha256` — Content hashing
- `net/http` — GitHub API calls
- `path/filepath` — Path security checks
- `database/sql` — Skill registration
- `github.com/google/uuid` — Skill IDs
