#!/usr/bin/env node
// build-post.mjs — emit a Facebook post skeleton with deterministic structure
// based on intent + voice. Agent fills in {{HOOK}}, {{BODY}}, {{CTA}} slots.
//
// Usage:
//   node scripts/build-post.mjs --intent=sales [--brand="HYFA"] [--audience="anh em cầu lông"]
//
// Output: JSON { template, slots, hashtags_suggested }
//
// Intent options: sales | story | announcement | gratitude
//   Defaults to sales when intent missing or unknown.

const TEMPLATES = {
  sales: {
    template: [
      "🔥 {{HOOK}}",
      "",
      "{{BODY}}",
      "",
      "📩 {{CTA}}",
      "{{HASHTAGS}}",
    ].join("\n"),
    slots: ["HOOK", "BODY", "CTA", "HASHTAGS"],
    cta_examples: ["Inbox em để đặt hàng nha", "Comment 'OK' để em giữ chỗ", "Click link để xem chi tiết"],
    hook_style: "Câu giật mình hoặc câu hỏi về pain point",
  },
  story: {
    template: [
      "{{HOOK}}",
      "",
      "{{BODY}}",
      "",
      "❤️ {{CTA}}",
    ].join("\n"),
    slots: ["HOOK", "BODY", "CTA"],
    cta_examples: ["Tag bạn nào thấy giống mình", "Share lưu cho mình", "Comment để em biết anh chị nghĩ sao"],
    hook_style: "1 câu kéo người đọc vào câu chuyện",
  },
  announcement: {
    template: [
      "📢 {{HOOK}}",
      "",
      "{{BODY}}",
      "",
      "✅ {{CTA}}",
    ].join("\n"),
    slots: ["HOOK", "BODY", "CTA"],
    cta_examples: ["RSVP qua inbox", "Lưu lại để khỏi quên", "Inbox em để biết thêm"],
    hook_style: "Tagline ngắn 1 dòng + emoji loa",
  },
  gratitude: {
    template: [
      "🙏 {{HOOK}}",
      "",
      "{{BODY}}",
      "",
      "⭐ {{CTA}}",
    ].join("\n"),
    slots: ["HOOK", "BODY", "CTA"],
    cta_examples: ["Cho em xin 1 review nhẹ", "Share giùm em nha", "Nhận quà nhỏ tại quầy"],
    hook_style: "Câu cảm ơn cụ thể, không sáo rỗng",
  },
};

function parseArgs(argv) {
  const out = {};
  for (const a of argv.slice(2)) {
    const m = a.match(/^--([^=]+)=(.*)$/);
    if (m) out[m[1]] = m[2];
  }
  return out;
}

function suggestHashtags({ brand, audience }) {
  const tags = [];
  if (brand) tags.push("#" + brand.replace(/\s+/g, "").toLowerCase());
  if (audience) {
    const slug = audience
      .toLowerCase()
      .normalize("NFD").replace(/[̀-ͯ]/g, "")
      .replace(/[^a-z0-9]+/g, "");
    if (slug) tags.push("#" + slug);
  }
  return tags;
}

function main() {
  const args = parseArgs(process.argv);
  const intent = TEMPLATES[args.intent] ? args.intent : "sales";
  const spec = TEMPLATES[intent];
  const hashtags = suggestHashtags({ brand: args.brand, audience: args.audience });

  console.log(JSON.stringify({
    status: "success",
    intent,
    template: spec.template,
    slots: spec.slots,
    hook_style: spec.hook_style,
    cta_examples: spec.cta_examples,
    hashtags_suggested: hashtags,
  }, null, 2));
}

main();
