#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const templatePath = path.join(
  repoRoot,
  "internal/tools/tuyettruong/seeds/shop-bot-template.md",
);

const ADMIN_TOOLS = [
  "shop_product_search",
  "shop_product_get",
  "shop_product_create",
  "shop_product_update",
  "shop_variant_update",
  "shop_product_delete",
  "shop_product_lookup_existing",
  "shop_product_draft_from_extracted",
  "shop_order_list",
  "shop_order_update_status",
  "web_fetch",
  "web_search",
];

const SHARED_TOOLS = ["shop_whoami"];

const SALES_TOOLS = [
  "shop_catalog_search",
  "shop_quote_add_item",
  "shop_quote_remove_item",
  "shop_quote_view",
  "shop_quote_set_customer",
  "shop_quote_finalize",
  "shop_quote_clear",
  "shop_order_place",
  "shop_order_customer_claimed_paid",
  "shop_order_lookup",
  "shop_notify_admin",
];

function usage() {
  console.error(`Usage:
  node scripts/provision-shop-agent.mjs --config internal/tools/tuyettruong/seeds/shop-bot.example.json > /tmp/shop.sql

The script prints SQL only. It does not connect to GoClaw or restart channels.`);
}

function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      args.help = true;
    } else if (arg === "--config") {
      args.config = argv[++i];
    } else {
      throw new Error(`unknown argument: ${arg}`);
    }
  }
  return args;
}

function readConfig(configPath) {
  if (!configPath) throw new Error("--config is required");
  const raw = fs.readFileSync(configPath, "utf8");
  return JSON.parse(raw);
}

function requireString(cfg, key) {
  const value = cfg[key];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`config.${key} is required`);
  }
  return value.trim();
}

function sqlString(value) {
  if (value == null) return "NULL";
  return `'${String(value).replaceAll("'", "''")}'`;
}

function sqlJson(value) {
  return `${sqlString(JSON.stringify(value))}::jsonb`;
}

function renderTemplate(template, cfg) {
  const values = {
    shop_name: cfg.shopName,
    shop_slug: cfg.shopSlug,
    language: cfg.language,
    brand_voice: cfg.brandVoice,
    regional_tone: cfg.regionalTone,
    payment_info: cfg.paymentInfo,
    return_policy: cfg.returnPolicy,
    escalation_policy: cfg.escalationPolicy,
  };
  return template.replace(/\{\{([a-z_]+)\}\}/g, (_, key) => {
    if (!(key in values)) throw new Error(`missing template value: ${key}`);
    return values[key];
  });
}

function buildIdentity(cfg) {
  return `# IDENTITY.md - ${cfg.displayName}

- **Name:** ${cfg.displayName}
- **Role:** AI shop operator for ${cfg.shopName}
- **Purpose:** Handle authenticated admin operations and customer sales support for one shop.
- **Scope:** This agent only serves ${cfg.shopName}. It must not use personal/team workflows from other agents.
- **Safety:** Resolve identity with shop_whoami before using admin tools. Default to Sales Mode when unsure.
`;
}

function buildCapabilities(cfg) {
  return `# CAPABILITIES.md - ${cfg.shopName}

## Admin

- Search products, inspect variants, update prices and stock.
- List orders and update order status after shop owner confirmation.
- Create inactive product drafts from product photos or extracted fields.

## Sales

- Search public products.
- Build quotes from live prices.
- Collect customer details and place orders.
- Record customer payment claims without confirming payment.

## Boundaries

- No destructive admin operation without confirm token.
- No invented price, SKU, stock, cost, or order status.
- No cross-shop data access.
`;
}

function buildShopConfig(cfg) {
  return `# SHOP_CONFIG.md

- **Shop:** ${cfg.shopName}
- **Slug:** ${cfg.shopSlug}
- **Language:** ${cfg.language}
- **Brand voice:** ${cfg.brandVoice}
- **Regional tone:** ${cfg.regionalTone}
- **Payment info:** ${cfg.paymentInfo}
- **Return policy:** ${cfg.returnPolicy}
- **Escalation:** ${cfg.escalationPolicy}
`;
}

function buildUserPredefined(cfg) {
  return `# USER_PREDEFINED.md - ${cfg.shopName}

This agent serves admins, staff, and customers of ${cfg.shopName}.

- Admin/staff identities must be resolved by shop_whoami.
- Unknown users are customers until proven otherwise.
- Keep Zalo replies plain-text and concise.
- Use regional wording from SHOP_CONFIG.md with restraint. Do not overuse "nghen".
`;
}

function validateConfig(cfg) {
  const normalized = {
    tenant: requireString(cfg, "tenant"),
    shopSlug: requireString(cfg, "shopSlug"),
    shopName: requireString(cfg, "shopName"),
    agentKey: requireString(cfg, "agentKey"),
    displayName: requireString(cfg, "displayName"),
    ownerId: requireString(cfg, "ownerId"),
    provider: requireString(cfg, "provider"),
    model: requireString(cfg, "model"),
    language: requireString(cfg, "language"),
    brandVoice: requireString(cfg, "brandVoice"),
    regionalTone: requireString(cfg, "regionalTone"),
    paymentInfo: requireString(cfg, "paymentInfo"),
    returnPolicy: requireString(cfg, "returnPolicy"),
    escalationPolicy: requireString(cfg, "escalationPolicy"),
    channel: cfg.channel ?? null,
  };

  if (!/^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$/.test(normalized.agentKey)) {
    throw new Error("config.agentKey must be kebab-case, 3-100 chars");
  }
  if (!/^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$/.test(normalized.shopSlug)) {
    throw new Error("config.shopSlug must be kebab-case, 3-100 chars");
  }
  if (normalized.channel) {
    if (typeof normalized.channel !== "object") throw new Error("config.channel must be an object");
    for (const key of ["name", "displayName", "type"]) {
      if (typeof normalized.channel[key] !== "string" || normalized.channel[key].trim() === "") {
        throw new Error(`config.channel.${key} is required`);
      }
    }
    if (!["telegram", "zalo_personal", "zalo_oa"].includes(normalized.channel.type)) {
      throw new Error("config.channel.type must be telegram, zalo_personal, or zalo_oa");
    }
  }
  return normalized;
}

function buildSQL(cfg) {
  const template = fs.readFileSync(templatePath, "utf8");
  const prompt = renderTemplate(template, cfg);
  const allTools = [...ADMIN_TOOLS, ...SHARED_TOOLS, ...SALES_TOOLS];
  const frontmatter = [
    `Shop bot for ${cfg.shopName}.`,
    "Resolves chat identity with shop_whoami.",
    "Admin/staff can manage products and orders.",
    "Customers get sales quote/order support only.",
  ].join(" ");

  const contextFiles = {
    "IDENTITY.md": buildIdentity(cfg),
    "CAPABILITIES.md": buildCapabilities(cfg),
    "SHOP_CONFIG.md": buildShopConfig(cfg),
    "USER_PREDEFINED.md": buildUserPredefined(cfg),
    "SHOP_BOT_TEMPLATE.md": prompt,
  };

  const channel = cfg.channel;
  const channelConfig = channel
    ? {
        dm_policy: channel.allowFrom?.length ? "allowlist" : "disabled",
        allow_from: channel.allowFrom ?? [],
        group_policy: "disabled",
        require_mention: false,
        dm_stream: true,
        reasoning_stream: true,
      }
    : null;

  const lines = [];
  lines.push("-- Generated by scripts/provision-shop-agent.mjs");
  lines.push("-- Review before applying. Channel credentials are intentionally empty.");
  lines.push("BEGIN;");
  lines.push("");
  lines.push("WITH tenant_row AS (");
  lines.push(`  SELECT id AS tenant_id FROM tenants WHERE slug = ${sqlString(cfg.tenant)} OR name = ${sqlString(cfg.tenant)} LIMIT 1`);
  lines.push("), upsert_agent AS (");
  lines.push("  INSERT INTO agents (");
  lines.push("    tenant_id, agent_key, display_name, owner_id, provider, model,");
  lines.push("    workspace, tools_config, other_config, agent_type, status, frontmatter,");
  lines.push("    max_tool_iterations, context_window, created_at, updated_at");
  lines.push("  )");
  lines.push("  SELECT");
  lines.push("    tenant_id,");
  lines.push(`    ${sqlString(cfg.agentKey)},`);
  lines.push(`    ${sqlString(cfg.displayName)},`);
  lines.push(`    ${sqlString(cfg.ownerId)},`);
  lines.push(`    ${sqlString(cfg.provider)},`);
  lines.push(`    ${sqlString(cfg.model)},`);
  lines.push(`    ${sqlString(`/app/workspace/shops/${cfg.shopSlug}`)},`);
  lines.push(`    ${sqlJson({ allow: allTools })},`);
  lines.push(`    ${sqlJson({ temperature: 0.2, prompt_mode: "full", shop_slug: cfg.shopSlug })},`);
  lines.push("    'predefined',");
  lines.push("    'active',");
  lines.push(`    ${sqlString(frontmatter)},`);
  lines.push("    12,");
  lines.push("    200000,");
  lines.push("    now(), now()");
  lines.push("  FROM tenant_row");
  lines.push("  ON CONFLICT (tenant_id, agent_key) WHERE deleted_at IS NULL DO UPDATE SET");
  lines.push("    display_name = EXCLUDED.display_name,");
  lines.push("    owner_id = EXCLUDED.owner_id,");
  lines.push("    provider = EXCLUDED.provider,");
  lines.push("    model = EXCLUDED.model,");
  lines.push("    workspace = EXCLUDED.workspace,");
  lines.push("    tools_config = EXCLUDED.tools_config,");
  lines.push("    other_config = EXCLUDED.other_config,");
  lines.push("    agent_type = EXCLUDED.agent_type,");
  lines.push("    status = EXCLUDED.status,");
  lines.push("    frontmatter = EXCLUDED.frontmatter,");
  lines.push("    max_tool_iterations = EXCLUDED.max_tool_iterations,");
  lines.push("    updated_at = now()");
  lines.push("  RETURNING id, tenant_id");
  lines.push(")");

  const entries = Object.entries(contextFiles);
  entries.forEach(([fileName, content], index) => {
    lines.push(`, context_${index} AS (`);
    lines.push("  INSERT INTO agent_context_files (agent_id, tenant_id, file_name, content, created_at, updated_at)");
    lines.push(`  SELECT id, tenant_id, ${sqlString(fileName)}, ${sqlString(content)}, now(), now() FROM upsert_agent`);
    lines.push("  ON CONFLICT (agent_id, file_name) DO UPDATE SET");
    lines.push("    content = EXCLUDED.content,");
    lines.push("    updated_at = now()");
    lines.push("  RETURNING id");
    lines.push(")");
  });

  if (channel) {
    const enabled = channel.enabled === true ? "true" : "false";
    lines.push(", upsert_channel AS (");
    lines.push("  INSERT INTO channel_instances (");
    lines.push("    tenant_id, name, display_name, channel_type, agent_id,");
    lines.push("    credentials, config, enabled, created_by, created_at, updated_at");
    lines.push("  )");
    lines.push("  SELECT");
    lines.push("    tenant_id,");
    lines.push(`    ${sqlString(channel.name)},`);
    lines.push(`    ${sqlString(channel.displayName)},`);
    lines.push(`    ${sqlString(channel.type)},`);
    lines.push("    id,");
    lines.push("    NULL,");
    lines.push(`    ${sqlJson(channelConfig)},`);
    lines.push(`    ${enabled},`);
    lines.push(`    ${sqlString(cfg.ownerId)},`);
    lines.push("    now(), now()");
    lines.push("  FROM upsert_agent");
    lines.push("  ON CONFLICT (tenant_id, name) DO UPDATE SET");
    lines.push("    display_name = EXCLUDED.display_name,");
    lines.push("    channel_type = EXCLUDED.channel_type,");
    lines.push("    agent_id = EXCLUDED.agent_id,");
    lines.push("    config = EXCLUDED.config,");
    lines.push("    enabled = EXCLUDED.enabled,");
    lines.push("    updated_at = now()");
    lines.push("  RETURNING id");
    lines.push(")");
  }

  lines.push("SELECT");
  lines.push("  (SELECT id FROM upsert_agent) AS agent_id,");
  lines.push(`  ${channel ? "(SELECT id FROM upsert_channel)" : "NULL"} AS channel_instance_id;`);
  lines.push("");
  lines.push("-- COMMIT; -- enable after reviewing output");
  lines.push("ROLLBACK;");
  lines.push("");
  lines.push("-- Next steps:");
  lines.push("-- 1. Apply with COMMIT after review.");
  lines.push("-- 2. Add encrypted channel credentials via GoClaw UI/API.");
  lines.push("-- 3. Enable the channel only after a smoke test allowlist is set.");
  return `${lines.join("\n")}\n`;
}

try {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    usage();
    process.exit(0);
  }
  const cfg = validateConfig(readConfig(args.config));
  process.stdout.write(buildSQL(cfg));
} catch (err) {
  console.error(`ERROR: ${err.message}`);
  usage();
  process.exit(1);
}
