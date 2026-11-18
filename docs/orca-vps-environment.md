# Orca workspace on the existing VPS

`orca.yaml` defines the `tuyetchinh-vps` SSH recipe. It connects to the existing
VPS through the local `tuyetchinh-vps` SSH alias. The recipe creates a separate
primary checkout at `/root/orca-workspaces/goclaw`; Orca creates linked worktrees
for workspaces. It does not modify the production checkout at `/opt/goclaw`.
The checkout uses `tuyetchinh1501/goclaw` as `origin` so Git push checks can
succeed; pull requests target `nextlevelbuilder/goclaw`.

The VPS is persistent. `destroy: none` disconnects Orca but does not remove the
server or checkout. All Orca workspaces on this recipe share the same host,
system packages, disk, and root account. Use a separate VM provider if full
machine isolation is required.

## First-time preparation

After reviewing the commands in `scripts/orca-vm/bootstrap-vps.sh`, run it on the
VPS as root. It installs Go, Node, pnpm, Codex, and GitHub CLI. It never reads or
writes a token. Sign in to GitHub CLI and Codex on the VPS interactively:

```bash
gh auth login
gh auth setup-git
codex login --device-auth
```

Do not copy desktop agent state or Git tokens to the VPS. Orca's SSH host key
must already be verified in the desktop's `known_hosts`.

## Checks

From the Windows checkout, run:

```powershell
orca vm recipe doctor tuyetchinh-vps --json
orca vm recipe doctor tuyetchinh-vps --provision --json
```

The second command creates the VPS checkout if it is absent. From an Orca
terminal inside the VPS workspace, run:

```bash
orca vm env doctor --run-setup --json
```

`scripts.setup` runs `scripts/orca-vm/setup.sh` to download Go modules and
install both UI dependency sets with frozen lockfiles. The Orca composer reads
recipes from the primary checkout, so this recipe appears in the picker only
after `orca.yaml` reaches that checkout.
