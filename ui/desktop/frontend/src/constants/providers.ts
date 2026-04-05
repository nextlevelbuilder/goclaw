export interface ProviderTypeInfo {
  value: string
  label: string
  apiBase: string
  needsKey: boolean
}

export const PROVIDER_TYPES: ProviderTypeInfo[] = [
  { value: 'anthropic_native', label: 'Anthropic (Native)', apiBase: '', needsKey: true },
  { value: 'openai_compat', label: 'OpenAI Compatible', apiBase: '', needsKey: true },
  { value: 'gemini_native', label: 'Google Gemini', apiBase: 'https://generativelanguage.googleapis.com/v1beta/openai', needsKey: true },
  { value: 'openrouter', label: 'OpenRouter', apiBase: 'https://openrouter.ai/api/v1', needsKey: true },
  { value: 'groq', label: 'Groq', apiBase: 'https://api.groq.com/openai/v1', needsKey: true },
  { value: 'deepseek', label: 'DeepSeek', apiBase: 'https://api.deepseek.com/v1', needsKey: true },
  { value: 'mistral', label: 'Mistral AI', apiBase: 'https://api.mistral.ai/v1', needsKey: true },
  { value: 'xai', label: 'xAI (Grok)', apiBase: 'https://api.x.ai/v1', needsKey: true },
  { value: 'minimax_native', label: 'MiniMax (Native)', apiBase: 'https://api.minimax.io/v1', needsKey: true },
  { value: 'novita', label: 'Novita AI', apiBase: 'https://api.novita.ai/openai', needsKey: true },
  { value: 'cohere', label: 'Cohere', apiBase: 'https://api.cohere.ai/compatibility/v1', needsKey: true },
  { value: 'perplexity', label: 'Perplexity', apiBase: 'https://api.perplexity.ai', needsKey: true },
  { value: 'dashscope', label: 'DashScope (Qwen)', apiBase: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', needsKey: true },
  { value: 'bailian', label: 'Bailian Coding', apiBase: 'https://coding-intl.dashscope.aliyuncs.com/v1', needsKey: true },
  { value: 'yescale', label: 'YesScale', apiBase: 'https://api.yescale.one/v1', needsKey: true },
  { value: 'zai', label: 'Z.ai API', apiBase: 'https://api.z.ai/api/paas/v4', needsKey: true },
  { value: 'zai_coding', label: 'Z.ai Coding Plan', apiBase: 'https://api.z.ai/api/coding/paas/v4', needsKey: true },
  { value: 'ollama', label: 'Ollama (Local)', apiBase: 'http://localhost:11434/v1', needsKey: false },
  { value: 'ollama_cloud', label: 'Ollama Cloud', apiBase: 'https://ollama.com/v1', needsKey: true },
  { value: 'claude_cli', label: 'Claude CLI (Local)', apiBase: '', needsKey: false },
  { value: 'acp', label: 'ACP Agent (Subprocess)', apiBase: '', needsKey: false },
]
