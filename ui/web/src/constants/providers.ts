export interface ProviderTypeInfo {
  value: string;
  label: string;
  apiBase: string;
  placeholder: string;
}

export const PROVIDER_TYPES: ProviderTypeInfo[] = [
  { value: "anthropic_native", label: "Anthropic (Native)", apiBase: "", placeholder: "https://api.anthropic.com" },
  { value: "openai_compat", label: "OpenAI Compatible", apiBase: "", placeholder: "https://api.openai.com/v1" },
  { value: "gemini_native", label: "Google Gemini", apiBase: "https://generativelanguage.googleapis.com/v1beta/openai", placeholder: "" },
  { value: "openrouter", label: "OpenRouter", apiBase: "https://openrouter.ai/api/v1", placeholder: "" },
  { value: "groq", label: "Groq", apiBase: "https://api.groq.com/openai/v1", placeholder: "" },
  { value: "deepseek", label: "DeepSeek", apiBase: "https://api.deepseek.com/v1", placeholder: "" },
  { value: "mistral", label: "Mistral AI", apiBase: "https://api.mistral.ai/v1", placeholder: "" },
  { value: "xai", label: "xAI (Grok)", apiBase: "https://api.x.ai/v1", placeholder: "" },
  { value: "minimax_native", label: "MiniMax (Native)", apiBase: "https://api.minimax.io/v1", placeholder: "" },
  { value: "cohere", label: "Cohere", apiBase: "https://api.cohere.ai/compatibility/v1", placeholder: "" },
  { value: "perplexity", label: "Perplexity", apiBase: "https://api.perplexity.ai", placeholder: "" },
  { value: "dashscope", label: "DashScope (Qwen)", apiBase: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", placeholder: "" },
  { value: "bailian", label: "Bailian Coding", apiBase: "https://coding-intl.dashscope.aliyuncs.com/v1", placeholder: "" },
];
