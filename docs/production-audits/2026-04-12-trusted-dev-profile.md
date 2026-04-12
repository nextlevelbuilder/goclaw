# Trusted Dev Profile for `coder`

Date: 2026-04-12

## Goal

Reduce avoidable shell blockers for the production `coder` agent while keeping
hard protections against commands that can damage the system, leak secrets, or
escape the runtime.

## Effective policy

Profile file: `scripts/agent-shell-profiles/trusted_dev.json`

Denied (`true`):

- `destructive_ops`
- `data_exfiltration`
- `reverse_shell`
- `code_injection`
- `privilege_escalation`
- `dangerous_paths`
- `env_injection`
- `container_escape`
- `crypto_mining`
- `filter_bypass`
- `network_recon`
- `persistence`
- `env_dump`

Allowed (`false`):

- `package_install`
- `process_control`

## Why this split

- Keep all groups that can destroy host/runtime state, escalate privileges,
  exfiltrate data, dump secrets, or open SSH/tunnel paths.
- Allow package installation because coding agents often need `npm install`,
  `pnpm add`, `pip install`, and similar dependency setup during legitimate
  development work.
- Allow process control because restarting or stopping local dev processes is a
  normal repair path during implementation and verification.

## Apply flow

From a machine that already has the GoClaw gateway token:

```bash
GOCLAW_GATEWAY_TOKEN=... \
  /Users/nguyenquocthong/project/goclaw/scripts/apply-agent-shell-profile.sh \
  trusted_dev \
  coder
```

The script resolves `coder` to its UUID, updates `shell_deny_groups` via
`PUT /v1/agents/{id}`, then reads the agent back to print the effective state.

## Production expectation

The `coder` agent should stop hitting blockers for dependency installation and
safe process restarts, but it must still refuse commands involving:

- `sudo`, `su`, mount/unshare, container escape patterns
- `rm -rf`, shutdown/reboot, disk formatting, fork bombs
- `curl POST`, `wget POST`, localhost data exfiltration patterns
- full environment dumps and direct `GOCLAW_*` secret reads
- `ssh`, `scp`, `sftp`, tunneling tools, or scanning utilities
