import { slugify } from "@/lib/slug";

export interface ProviderTypeInfo {
  value: string;
  label: string;
  apiBase: string;
  placeholder: string;
}

type ProviderAliasSource = string | { name?: string | null };

export const DEFAULT_CODEX_OAUTH_ALIAS = "openai-codex";

export const PROVIDER_TYPES: ProviderTypeInfo[] = [
  { value: "chatgpt_oauth", label: "ChatGPT Subscription (OAuth)", apiBase: "", placeholder: "" },
  { value: "anthropic_native", label: "Anthropic (Native)", apiBase: "", placeholder: "https://api.anthropic.com" },
  { value: "openai_compat", label: "OpenAI Compatible", apiBase: "", placeholder: "https://api.openai.com/v1" },
  { value: "gemini_native", label: "Google Gemini", apiBase: "https://generativelanguage.googleapis.com/v1beta/openai", placeholder: "" },
  { value: "openrouter", label: "OpenRouter", apiBase: "https://openrouter.ai/api/v1", placeholder: "" },
  { value: "groq", label: "Groq", apiBase: "https://api.groq.com/openai/v1", placeholder: "" },
  { value: "deepseek", label: "DeepSeek", apiBase: "https://api.deepseek.com/v1", placeholder: "" },
  { value: "mistral", label: "Mistral AI", apiBase: "https://api.mistral.ai/v1", placeholder: "" },
  { value: "xai", label: "xAI (Grok)", apiBase: "https://api.x.ai/v1", placeholder: "" },
  { value: "minimax_native", label: "MiniMax (Native)", apiBase: "https://api.minimax.io/v1", placeholder: "" },
  { value: "novita", label: "Novita AI", apiBase: "https://api.novita.ai/openai", placeholder: "" },
  { value: "cohere", label: "Cohere", apiBase: "https://api.cohere.ai/compatibility/v1", placeholder: "" },
  { value: "perplexity", label: "Perplexity", apiBase: "https://api.perplexity.ai", placeholder: "" },
  { value: "dashscope", label: "DashScope (Qwen)", apiBase: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", placeholder: "" },
  { value: "bailian", label: "Bailian Coding", apiBase: "https://coding-intl.dashscope.aliyuncs.com/v1", placeholder: "" },
  { value: "yescale", label: "YesScale", apiBase: "https://api.yescale.one/v1", placeholder: "" },
  { value: "zai", label: "Z.ai API", apiBase: "https://api.z.ai/api/paas/v4", placeholder: "" },
  { value: "zai_coding", label: "Z.ai Coding Plan", apiBase: "https://api.z.ai/api/coding/paas/v4", placeholder: "" },
  { value: "byteplus", label: "BytePlus ModelArk", apiBase: "https://ark.ap-southeast.bytepluses.com/api/v3", placeholder: "" },
  { value: "byteplus_coding", label: "BytePlus Coding Plan", apiBase: "https://ark.ap-southeast.bytepluses.com/api/coding/v3", placeholder: "" },
  { value: "ollama", label: "Ollama (Local)", apiBase: "http://localhost:11434/v1", placeholder: "" },
  { value: "ollama_cloud", label: "Ollama Cloud", apiBase: "https://ollama.com/v1", placeholder: "" },
  { value: "claude_cli", label: "Claude CLI (Local)", apiBase: "", placeholder: "" },
  { value: "acp", label: "ACP Agent (Subprocess)", apiBase: "", placeholder: "claude" },
];

function providerAliasName(value: ProviderAliasSource): string {
  if (typeof value === "string") return value.trim().toLowerCase();
  return value.name?.trim().toLowerCase() ?? "";
}

export function suggestUniqueProviderAlias(
  existingProviders: ProviderAliasSource[],
  options?: { baseAlias?: string; excludeName?: string },
): string {
  const baseAlias = slugify(options?.baseAlias ?? DEFAULT_CODEX_OAUTH_ALIAS) || DEFAULT_CODEX_OAUTH_ALIAS;
  const taken = new Set(existingProviders.map(providerAliasName).filter(Boolean));
  const excludeName = providerAliasName(options?.excludeName ?? "");

  if (excludeName) taken.delete(excludeName);
  if (!taken.has(baseAlias)) return baseAlias;

  let suffix = 2;
  while (taken.has(`${baseAlias}-${suffix}`)) {
    suffix += 1;
  }
  return `${baseAlias}-${suffix}`;
}
