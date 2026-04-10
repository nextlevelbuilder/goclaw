/** Curated 1536-dimension embedding models per provider type. */
export const EMBEDDING_MODELS: Record<string, { id: string; name: string }[]> = {
  openai_compat: [
    { id: "text-embedding-3-small", name: "text-embedding-3-small (1536d)" },
    { id: "text-embedding-3-large", name: "text-embedding-3-large (3072d → 1536 via dimensions)" },
    { id: "text-embedding-ada-002", name: "text-embedding-ada-002 (1536d)" },
  ],
  openrouter: [
    { id: "openai/text-embedding-3-small", name: "openai/text-embedding-3-small (1536d)" },
    { id: "openai/text-embedding-3-large", name: "openai/text-embedding-3-large (3072d → 1536)" },
    { id: "openai/text-embedding-ada-002", name: "openai/text-embedding-ada-002 (1536d)" },
  ],
  gemini_native: [
    { id: "gemini-embedding-001", name: "gemini-embedding-001 (3072d → 1536 via dimensions)" },
  ],
  mistral: [
    { id: "codestral-embed", name: "codestral-embed (1536d default)" },
  ],
  dashscope: [
    { id: "text-embedding-v3", name: "text-embedding-v3 (1536 via dimensions)" },
  ],
  cohere: [
    { id: "embed-v4", name: "embed-v4 (1536d native)" },
  ],
};

export const DEFAULT_EMBEDDING_MODELS: { id: string; name: string }[] = [];

export interface InitState {
  embProvider: string;
  embModel: string;
  embMaxChunkLen: string;
  embChunkOverlap: string;
  toolStatus: boolean;
  blockReply: boolean;
  intentClassify: boolean;
  compProvider: string;
  compModel: string;
  compThreshold: string;
  compKeepRecent: string;
  compMaxTokens: string;
  kgProvider: string;
  kgModel: string;
  kgMinConfidence: string;
  bgProvider: string;
  bgModel: string;
}

export const DEFAULTS: InitState = {
  embProvider: "", embModel: "",
  embMaxChunkLen: "", embChunkOverlap: "",
  toolStatus: true, blockReply: false, intentClassify: true,
  compProvider: "", compModel: "",
  compThreshold: "", compKeepRecent: "", compMaxTokens: "",
  kgProvider: "", kgModel: "", kgMinConfidence: "0.75",
  bgProvider: "", bgModel: "",
};

export function parseBool(v: string | undefined, fallback: boolean): boolean {
  if (v === undefined) return fallback;
  return v !== "false" && v !== "0";
}
