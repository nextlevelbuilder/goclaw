# Meow image transport — verified decision

How an offline-generated post image (Mac) reaches a path the **containerized**
publish path can `os.Open`. This is the riskiest assumption in the Meow draft
pipeline, so it was proven on the real VPS before any ingest code was written.

## Runtime facts (Contabo VPS, verified 2026-06-14)
- The gateway runs in Docker (`goclaw-goclaw-1`); `/app/data` is the named
  volume `goclaw_goclaw-data` (mountpoint `/var/lib/docker/volumes/goclaw_goclaw-data/_data`), **not** a host bind mount.
- The app process runs as **uid 1000**; data files are owned `1000:1000`.
- The container is started with **`CapDrop=[ALL]`** and **no** user-namespace
  remap. Consequence: even `docker exec -u 0` (in-container "root") **cannot**
  write into the 1000-owned volume — it lacks `CAP_DAC_OVERRIDE`.

## Decision: transport via the host volume path
Write image bytes from the host, then let the container read them through the
volume. Do **not** use `docker exec` to create files (caps dropped), and do not
add a host bind mount.

```
host:      /var/lib/docker/volumes/goclaw_goclaw-data/_data/meow-assets/drafts/<channel>/<date>.webp
container: /app/data/meow-assets/drafts/<channel>/<date>.webp
owner/mode: 1000:1000, 0644
```

Steps (host root on the VPS):
1. Land the WebP on the VPS (`scp` for the manual MVP; `rsync` later).
2. `mkdir -p` the per-channel drafts dir under the volume; `cp` the bytes in.
3. `chown -R 1000:1000` the `meow-assets` tree; `chmod 0644` the file.

The ingest step records `mp_content_posts.image_path` as the **container**
path (`/app/data/meow-assets/drafts/...`), never a Mac path.

## Why this passes ValidateImagePath
The publish path's allowed image root is `<dataDir>/meow-assets` (wired in
`cmd/gateway.go` / `cmd/gateway_channels_setup.go`). In the container
`dataDir = /app/data`, so the allowed root is `/app/data/meow-assets` and every
draft image lives under it. `meow.ValidateImagePath` resolves symlinks and
asserts containment, so a correctly transported file is accepted while
traversal/symlink-escape is rejected (unit-tested in `internal/meow`).

## Spike proof (2026-06-14)
A 16×16 WebP generated on the Mac was `scp`'d to the VPS, copied into
`…/_data/meow-assets/drafts/spike/spike.webp`, `chown 1000:1000`, `chmod 0644`.
The running `goclaw-goclaw-1` container then saw it at
`/app/data/meow-assets/drafts/spike/spike.webp` (owner `1000 1000`) and a
process running as **uid 1000 read it successfully** with the `RIFF…WEBP` magic
bytes intact. The test file was removed; the `meow-assets` root remains for draft images.

## Operator note
The manual MVP transport is `scp` + host-root `cp` into the volume. A future
upload tool can automate the host-side copy, but the destination + ownership
contract above is fixed.
