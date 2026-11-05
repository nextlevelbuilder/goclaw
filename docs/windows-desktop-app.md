# Windows Desktop App (full edition)

A native Windows window around the full GoClaw gateway (PostgreSQL, all features),
shipped as one installer. It is distinct from the Lite desktop app in `ui/desktop/`
(SQLite, limited edition). Intended for a single user on one machine.

## What gets installed

| File | Role |
|---|---|
| `GoClawDesktop.exe` | The window (Wails + WebView2) and the server supervisor. Source: `ui/desktop-full/` |
| `goclaw-server.exe` | The gateway with the web UI embedded (`-tags embedui`), same code as `make build-full` |
| `pg_dump.exe` | Shim that forwards `pg_dump` into the PostgreSQL container; lets the built-in `goclaw backup` work without PostgreSQL client tools |
| `goclaw.ico` | Icon loaded by the window at runtime |

Everything is under `%ProgramFiles%\GoClaw` and read-only at runtime. The shell name
differs from the legacy `goclaw.exe` on purpose: Windows file and process names are
case-insensitive, so `GoClaw.exe` would collide with it.

## How it runs

- The window shows a small start page, starts the server when needed, then loads
  `http://127.0.0.1:<port>/` (default port **18800**; set `GOCLAW_PORT` in the
  settings to change it).
- The server runs **detached** from the window. Closing the window does not stop it,
  so Telegram bots and cron jobs keep running. It stops through the app menu
  (File > Quit and stop server), the Start Menu shortcut "Quit GoClaw",
  `GoClawDesktop.exe --quit`, or the installer/uninstaller. A graceful stop is
  tried first; a hard kill happens only after 30 seconds.
- `GoClawDesktop.exe --import <folder>` imports settings without the dialog (the
  result is logged by key name only, in `shell.log`).
- `GoClawDesktop.exe --background` starts the server without a window; `--minimized` also
  opens the window, minimized to the taskbar; `--settings` opens the window on the
  settings page (File > Settings..., or the Start Menu shortcut "GoClaw Settings").
- Settings page: "Start with Windows" and "Open the window minimized" (the per-user `Run`
  entry runs `--background` or `--minimized`), server status with Restart and Stop, shortcuts to
  the logs and backups folders, and "Import settings again". It is served by the app itself
  (`ui/desktop-full/assets/settings/`); Wails injects its bridge only into pages whose URL ends
  in `/`, so the page lives at `/settings/`.
- The server binds `127.0.0.1` only unless the settings say otherwise.
- A running server is reused only when it is this app's own binary (pid file match).
  If another program holds the port, the window names it instead of attaching.
- Needs PostgreSQL 18 (pgvector) reachable at the host and port in the DSN, and for
  the backup shim Docker with a container named `goclaw-postgres`
  (override with `GOCLAW_PG_CONTAINER`).

## Settings and data

| What | Where |
|---|---|
| Settings (environment) | `%APPDATA%\GoClaw\goclaw.env` |
| Gateway config | `%APPDATA%\GoClaw\config.json` (also the server's working directory) |
| Window size and position | `%APPDATA%\GoClaw\window.json` |
| Server log | `GOCLAW_LOG_FILE` if set in the settings, else `%LOCALAPPDATA%\GoClaw\logs\server.log` |
| Shell log, server stderr | `%LOCALAPPDATA%\GoClaw\logs\shell.log`, `server-stderr.log` |
| Pre-upgrade backups | `%LOCALAPPDATA%\GoClaw\backups\` |
| Pid file | `%LOCALAPPDATA%\GoClaw\server.pid` |

On first start the window asks for the folder of an existing installation. It merges
`.env` then `.env.local` (later wins) and copies `config.json`; the originals are not
changed and **no encryption key is ever generated**: the stored provider keys in the
database can only be read with the key from the existing settings. `GOCLAW_*`
variables of the launching environment are ignored; only the settings file counts.
If `GOCLAW_ALLOWED_ORIGINS` is non-empty, the window's origin
(`http://127.0.0.1:<port>`) is appended automatically.

The settings file holds the database password, encryption key and provider keys in
plain text. It is created readable by the current Windows user only; treat the
machine and account as the trust boundary.

## Database upgrades

If the server refuses to start because the schema is older than the binary, the
window offers "Back up and upgrade": it runs `goclaw-server.exe backup`, and only when
that succeeds starts the server once with `GOCLAW_AUTO_UPGRADE=true`. A database that
is newer than the binary is reported as an error and never touched. The backup needs
the `pg_dump` shim, so Docker must be running.

## Build

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-winapp.ps1
```

Options: `-Version`, `-OutDir` (inside the repo, default `winapp-out`), `-SkipWeb`
(reuse the embedded web UI), `-NoInstaller`. `make winapp` calls the same script.
Requires Go, Node with pnpm, and NSIS (`winget install NSIS.NSIS`). The output is
staged and replaced only when every binary exists and the server reports the expected
version. See `scripts/build-winapp.ps1` and `ui/desktop-full/installer/goclaw.nsi`.

## Install and uninstall

Run `GoClaw-Setup-<version>.exe` (UAC prompt; per-machine). It requires the WebView2
runtime (included in Windows 11), and refuses to continue while the window is open.
Uninstalling stops the server and removes the program files, shortcuts and the
Add/Remove Programs entry; settings, logs and backups are kept unless you answer yes
to the prompt. Turn off "Start with Windows" in the settings page before uninstalling:
a per-user `Run` entry cannot be removed for other accounts and would point at a
missing file. The installer is unsigned, so SmartScreen warns on first run.

## Moving from a hand-run gateway

The app must replace the old gateway, not run beside it: both would use the same
database and the same bot tokens, so Telegram would be polled twice and cron jobs
would fire twice. Stop the old gateway **and** whatever supervisor restarts it before
the first start of the app (for the maintainer's setup: the scheduled task "GoClaw
Gateway" and `start-goclaw.ps1`, then `goclaw.exe`), then import its folder.

## Known limits

- No tray icon (Wails v2 has none); the server is controlled through the menu,
  shortcuts and `--quit`.
- No auto-update and no code signing.
- The shell executable has no embedded icon resource; Explorer shows the default
  icon for it, while shortcuts, the installer, the title bar and the taskbar use
  the GoClaw icon.
- Remote access is not provided; put a proxy or Tailscale in front of the loopback
  port if it is ever needed.
