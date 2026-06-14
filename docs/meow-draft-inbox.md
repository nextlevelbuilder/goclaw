# Meow draft inbox — upload convention

Where offline-prepped draft bundles must land on the server so the owner can
pull them into the draft queue with `/meow ingest`. This mirrors the host-volume
contract in `meow-draft-bundle.md` (bundle schema) and `meow-image-transport.md`
(image transport); read those first.

## Location (fixed)
The ingest command resolves its argument relative to the **inbox root**
`<dataDir>/meow-inbox`. In the container `dataDir = /app/data`, so:

```
host:      /var/lib/docker/volumes/goclaw_goclaw-data/_data/meow-inbox/<channel>/
container: /app/data/meow-inbox/<channel>/
owner/mode: 1000:1000 — dirs 0755, files 0644 (the app runs as uid 1000)
```

The inbox is a named-volume path, **not** a host bind mount, and the container
runs `CapDrop=[ALL]` — so land files from the **host** and `chown 1000:1000`;
`docker exec` cannot write the 1000-owned volume.

## Layout per bundle
One bundle = one channel-day. The bundle JSON and its WebP sit **side by side**
in the same directory; the JSON's `image` field is a **bare filename** (no path)
naming that WebP:

```
/app/data/meow-inbox/kingboardgames/
├── 2026-06-16.json     # DraftBundle (see meow-draft-bundle.md)
└── 2026-06-16.webp     # the image the JSON's "image" field names
```

## Transport steps (host root on the VPS)
1. `scp` the `<date>.json` + `<date>.webp` to the VPS (manual MVP; `rsync` later).
2. `mkdir -p` the per-channel dir under `…/_data/meow-inbox/` and `cp` both in.
3. `chown -R 1000:1000 …/_data/meow-inbox`; `chmod 0644` the files.

## Ingest
From the verified owner chat:

```
/meow ingest <channel>/<date>.json     e.g. /meow ingest kingboardgames/2026-06-16.json
```

The command joins the argument under the inbox root and runs
`meow.ValidateImagePath` against it: the JSON must exist, be a regular file, and
resolve **inside** the inbox root — a `..` or symlink escape is rejected before
any read. Ingest then validates the bundle, the per-channel button allowlist, and
the image's `RIFF…WEBP` signature; a content problem **holds** (no DB row).

On success it copies the image into the asset root
(`/app/data/meow-assets/drafts/<brandKey>/<date>.webp`) and upserts a `draft`
row whose `image_path` is that **container** asset path — never the inbox path.
The inbox copy can be cleaned up after a successful ingest; the published image
lives under `meow-assets`.

## Operator note
No upload tool yet — `scp` + host-root `cp` into the volume is the MVP. A future
tool can automate the host-side copy, but the inbox location + ownership +
bundle layout above are the fixed contract.
