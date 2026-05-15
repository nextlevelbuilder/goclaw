#!/usr/bin/env node
// build-prompt.mjs — deterministic prompt builder for product-swap skill.
// Output: JSON { prompt: <string>, input_images: <array> } that agent pipes
// straight into create_image.
//
// Usage:
//   node scripts/build-prompt.mjs \
//     --target=/path/scene.jpg \
//     --old="thùng sữa TH màu xanh trắng bên trái" \
//     --new-image=/path/heineken-ref.jpg \
//     [--new-text="thùng bia Heineken xanh lá"]
//
// Either --new-image OR --new-text is required. If both given, --new-image wins
// and --new-text becomes a description hint inside the prompt.

function parseArgs(argv) {
  const out = {};
  for (const a of argv.slice(2)) {
    const m = a.match(/^--([^=]+)=(.*)$/);
    if (m) out[m[1]] = m[2];
  }
  return out;
}

function fail(msg) {
  console.error(JSON.stringify({ status: "failed", error: msg }));
  process.exit(1);
}

function buildPrompt({ oldDesc, newDesc, hasRefImage }) {
  const lines = [];
  if (hasRefImage) {
    lines.push(
      `Trong ảnh tham chiếu thứ nhất, thay ${oldDesc} bằng vật trong ảnh tham chiếu thứ hai (lấy đúng màu sắc, label, hình dạng của vật trong ảnh 2).`,
    );
    if (newDesc) lines.push(`Vật mới có thể mô tả: ${newDesc}.`);
  } else {
    lines.push(`Trong ảnh tham chiếu, thay ${oldDesc} bằng ${newDesc}.`);
  }
  lines.push(
    "- Giữ nguyên: người trong ảnh, dáng đứng, biểu cảm, ánh sáng, bối cảnh, các vật khác xung quanh.",
    "- Vật mới phải có tỉ lệ và góc nhìn khớp với vị trí cũ.",
    "- Bóng đổ + phản chiếu trên bề mặt phải phù hợp với vật mới.",
    `- KHÔNG thay đổi gì ngoài ${oldDesc}.`,
    "- Chất lượng ảnh giữ đúng độ phân giải gốc.",
  );
  return lines.join("\n");
}

function main() {
  const args = parseArgs(process.argv);
  const target = args.target;
  const oldDesc = args.old;
  const newImage = args["new-image"];
  const newText = args["new-text"];

  if (!target) fail("missing --target=<path>");
  if (!oldDesc) fail("missing --old=<description of product to remove>");
  if (!newImage && !newText) fail("need at least one of --new-image=<path> or --new-text=<description>");

  const inputImages = [target];
  if (newImage) inputImages.push(newImage);

  const prompt = buildPrompt({
    oldDesc,
    newDesc: newText || "",
    hasRefImage: Boolean(newImage),
  });

  console.log(JSON.stringify({
    status: "success",
    prompt,
    input_images: inputImages,
    has_reference_image: Boolean(newImage),
  }));
}

main();
