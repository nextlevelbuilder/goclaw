#!/usr/bin/env node
// render-collage.mjs — deterministic N-image grid composer.
// Usage:
//   node scripts/render-collage.mjs \
//     --ratio=16:9 \
//     --images=/path/a.jpg,/path/b.jpg \
//     [--label="My Brand"] \
//     [--out=/path/out.png] \
//     [--gap=12] [--pad=32] [--bg=#ffffff]

import sharp from "sharp";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const CANVAS = {
  "16:9": { w: 1920, h: 1080 },
  "9:16": { w: 1080, h: 1920 },
  "1:1": { w: 1080, h: 1080 },
};

const MAX_IMAGES = 12;

function parseArgs(argv) {
  const out = {};
  for (const a of argv.slice(2)) {
    const m = a.match(/^--([^=]+)=(.*)$/);
    if (m) out[m[1]] = m[2];
  }
  return out;
}

function pickGrid(layouts, ratio, n) {
  const key = `${ratio}:${n}`;
  if (layouts[key]) return { cols: layouts[key][0], rows: layouts[key][1] };
  // Fallback: closest cols/rows ratio to canvas ratio, with cols*rows >= n.
  const c = CANVAS[ratio];
  const target = c.w / c.h;
  let best = null;
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols);
    const aspect = cols / rows;
    const score = Math.abs(Math.log(aspect / target));
    if (best === null || score < best.score) best = { cols, rows, score };
  }
  return { cols: best.cols, rows: best.rows };
}

async function loadPanel(file, w, h) {
  const buf = await fs.readFile(file);
  return sharp(buf)
    .resize(w, h, { fit: "cover", position: "centre" })
    .toBuffer();
}

function svgLabel(text, w, h) {
  // Bottom strip ~48px with semi-transparent black, white text.
  const stripH = 56;
  const fontSize = 28;
  const escaped = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  return Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}">
      <rect x="0" y="${h - stripH}" width="${w}" height="${stripH}" fill="rgba(0,0,0,0.55)"/>
      <text x="${w / 2}" y="${h - stripH / 2 + fontSize / 3}"
            font-family="Helvetica, Arial, sans-serif" font-size="${fontSize}"
            font-weight="600" fill="#ffffff" text-anchor="middle">${escaped}</text>
    </svg>`,
  );
}

function defaultOutPath() {
  const dateDir = new Date().toISOString().slice(0, 10);
  const ts = Date.now();
  return path.join(
    process.cwd(),
    "generated",
    dateDir,
    `collage-${ts}.png`,
  );
}

async function main() {
  const args = parseArgs(process.argv);
  const ratio = args.ratio || "16:9";
  if (!CANVAS[ratio]) {
    console.error(JSON.stringify({ status: "failed", error: `unsupported ratio ${ratio}, use one of: ${Object.keys(CANVAS).join(", ")}` }));
    process.exit(1);
  }
  if (!args.images) {
    console.error(JSON.stringify({ status: "failed", error: "missing --images=<comma-separated paths>" }));
    process.exit(1);
  }
  const images = args.images.split(",").map((s) => s.trim()).filter(Boolean);
  if (images.length < 2 || images.length > MAX_IMAGES) {
    console.error(JSON.stringify({ status: "failed", error: `image count ${images.length} not in [2, ${MAX_IMAGES}]` }));
    process.exit(1);
  }
  for (const f of images) {
    try { await fs.access(f); } catch {
      console.error(JSON.stringify({ status: "failed", error: `image not found: ${f}` }));
      process.exit(1);
    }
  }

  const gap = parseInt(args.gap || "12", 10);
  const pad = parseInt(args.pad || "32", 10);
  const bg = args.bg || "#ffffff";

  const layoutsPath = path.join(__dirname, "layouts.json");
  const layouts = JSON.parse(await fs.readFile(layoutsPath, "utf8"));
  const { cols, rows } = pickGrid(layouts, ratio, images.length);

  const canvas = CANVAS[ratio];
  const innerW = canvas.w - 2 * pad - (cols - 1) * gap;
  const innerH = canvas.h - 2 * pad - (rows - 1) * gap;
  const cellW = Math.floor(innerW / cols);
  const cellH = Math.floor(innerH / rows);

  // Render panels in row-major order; leave extra cells blank (when N < cols*rows).
  const composites = [];
  for (let i = 0; i < images.length; i++) {
    const r = Math.floor(i / cols);
    const c = i % cols;
    const x = pad + c * (cellW + gap);
    const y = pad + r * (cellH + gap);
    const buf = await loadPanel(images[i], cellW, cellH);
    composites.push({ input: buf, left: x, top: y });
  }

  if (args.label) {
    composites.push({ input: svgLabel(args.label, canvas.w, canvas.h), top: 0, left: 0 });
  }

  const outPath = args.out || defaultOutPath();
  await fs.mkdir(path.dirname(outPath), { recursive: true });
  await sharp({
    create: {
      width: canvas.w,
      height: canvas.h,
      channels: 4,
      background: bg,
    },
  })
    .composite(composites)
    .png()
    .toFile(outPath);

  console.log(JSON.stringify({
    status: "success",
    path: outPath,
    ratio,
    cols,
    rows,
    cells: cols * rows,
    images: images.length,
  }));
}

main().catch((err) => {
  console.error(JSON.stringify({ status: "failed", error: String(err && err.message || err) }));
  process.exit(1);
});
