# Script Quality Criteria

Scripts provide deterministic reliability and token efficiency.

## When to Include Scripts

- Same code rewritten repeatedly
- Deterministic operations needed
- Complex transformations
- External tool integrations

## Cross-Platform Requirements

**Prefer:** Node.js or Python
**Avoid:** Bash scripts (not well-supported on Windows)

If bash required, provide Node.js/Python alternative.

## Testing Requirements

**Mandatory:** All scripts must have tests

```bash
# Run tests before packaging
python -m pytest scripts/tests/
# or
npm test
```

Tests must pass. No skipping failed tests.

## Runtime Environment (GoClaw)

Scripts run inside the GoClaw container via the `exec` tool. Environment is set by the entrypoint:

| Variable | Value | Purpose |
|----------|-------|---------|
| `PYTHONPATH` | `/app/.goclaw/data/.runtime/pip` | Python runtime packages |
| `PIP_TARGET` | `/app/.goclaw/data/.runtime/pip` | pip install target |
| `NPM_CONFIG_PREFIX` | `/app/.goclaw/data/.runtime/npm-global` | npm global install dir |
| `NODE_PATH` | `/usr/local/lib/node_modules:...` | Node module resolution |

**Installing packages at runtime (no sudo needed):**
```bash
pip3 install <package>        # installs to PIP_TARGET, persists in volume
npm install -g <package>      # installs to NPM_CONFIG_PREFIX, persists in volume
```

Packages installed persist across tool calls within the same container lifecycle.

## Documentation Requirements

### .env.example
Show required variables without values:

```
API_KEY=
DATABASE_URL=
DEBUG=false
```

### requirements.txt (Python)
Pin major versions:

```
requests>=2.28.0
python-dotenv>=1.0.0
```

### package.json (Node.js)
Include scripts:

```json
{
  "scripts": {
    "test": "jest"
  }
}
```

## Manual Testing

Before packaging, test with real use cases:

```bash
# Example: PDF rotation script
python scripts/rotate_pdf.py input.pdf 90 output.pdf
```

Verify output matches expectations.

## Error Handling

- Clear error messages
- Graceful failures
- No silent errors
- Exit codes: 0 success, non-zero failure
