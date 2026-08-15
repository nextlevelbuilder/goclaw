export type SkillsTab = "core" | "custom";
export type SkillsFilter = "all" | "attention" | "missing-deps" | "disabled" | "archived" | "unmanaged";
export type SkillsSort = "name" | "deps" | "version";

export interface SkillsPageState {
  tab: SkillsTab;
  q: string;
  filter: SkillsFilter;
  sort: SkillsSort;
  agent: string | null;
}

const FILTERS = new Set<SkillsFilter>(["all", "attention", "missing-deps", "disabled", "archived", "unmanaged"]);
const SORTS = new Set<SkillsSort>(["name", "deps", "version"]);

export const DEFAULT_SKILLS_PAGE_STATE: SkillsPageState = {
  tab: "core",
  q: "",
  filter: "all",
  sort: "name",
  agent: null,
};

export function parseSkillsPageState(params: URLSearchParams): SkillsPageState {
  const rawFilter = params.get("filter");
  const rawSort = params.get("sort");
  const q = (params.get("q") ?? "").trim();
  const agent = (params.get("agent") ?? "").trim();

  return {
    tab: params.get("tab") === "custom" ? "custom" : "core",
    q,
    filter: rawFilter && FILTERS.has(rawFilter as SkillsFilter) ? (rawFilter as SkillsFilter) : "all",
    sort: rawSort && SORTS.has(rawSort as SkillsSort) ? (rawSort as SkillsSort) : "name",
    agent: agent || null,
  };
}

export function serializeSkillsPageState(
  currentParams: URLSearchParams,
  updates: Partial<SkillsPageState>,
): URLSearchParams {
  const next = new URLSearchParams(currentParams);
  const state = { ...parseSkillsPageState(currentParams), ...updates };

  setParam(next, "tab", state.tab === "core" ? "" : state.tab);
  setParam(next, "q", state.q.trim());
  setParam(next, "filter", state.filter === "all" ? "" : state.filter);
  setParam(next, "sort", state.sort === "name" ? "" : state.sort);
  setParam(next, "agent", state.agent ?? "");

  return next;
}

function setParam(params: URLSearchParams, key: string, value: string) {
  if (value) params.set(key, value);
  else params.delete(key);
}
