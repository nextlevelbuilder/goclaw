export interface SearchProviderSpec {
  /** Tailwind class for the card's colour rail. */
  rail: string;
  /** Whether the card shows an API key field: required, optional, or hidden. */
  apiKey: "required" | "optional" | "none";
  /** Keyless providers start switched off until the tenant turns them on. */
  optIn?: boolean;
  /** Always last and always on (DuckDuckGo); not sortable. */
  locked?: boolean;
}

/**
 * Web search providers known to the chain form, in default card order. Keep in
 * step with internal/tools: keyless providers (keylessSearchProviders there)
 * are opt-in here, and the card label comes from
 * builtin.searchChain.providers.<key>.
 */
const CATALOG = {
  exa: { rail: "bg-blue-600", apiKey: "required" },
  tavily: { rail: "bg-cyan-500", apiKey: "required" },
  brave: { rail: "bg-orange-500", apiKey: "required" },
  parallel: { rail: "bg-violet-500", apiKey: "none", optIn: true },
  serply: { rail: "bg-emerald-500", apiKey: "required" },
  keenable: { rail: "bg-indigo-500", apiKey: "optional", optIn: true },
  duckduckgo: { rail: "bg-slate-500", apiKey: "none", locked: true },
} satisfies Record<string, SearchProviderSpec>;

export type SearchProviderKey = keyof typeof CATALOG;

export const SEARCH_PROVIDERS: Record<SearchProviderKey, SearchProviderSpec> = CATALOG;

export const SORTABLE_SEARCH_PROVIDERS = (Object.keys(SEARCH_PROVIDERS) as SearchProviderKey[]).filter(
  (key) => !SEARCH_PROVIDERS[key].locked,
);
