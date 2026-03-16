import { ROUTES } from "./constants";

export interface FeatureDef {
  key: string;
  labelKey: string;
  descKey: string;
  route?: string;
}

export interface FeatureGroup {
  key: string;
  labelKey: string;
  descKey: string;
  features: FeatureDef[];
}

export const FEATURE_GROUPS: FeatureGroup[] = [
  {
    key: "capabilities",
    labelKey: "features:groups.capabilities",
    descKey: "features:groupDesc.capabilities",
    features: [
      { key: "skills", labelKey: "features:items.skills", descKey: "features:desc.skills", route: ROUTES.SKILLS },
      { key: "builtin_tools", labelKey: "features:items.builtinTools", descKey: "features:desc.builtinTools", route: ROUTES.BUILTIN_TOOLS },
      { key: "mcp", labelKey: "features:items.mcp", descKey: "features:desc.mcp", route: ROUTES.MCP },
      { key: "tts", labelKey: "features:items.tts", descKey: "features:desc.tts", route: ROUTES.TTS },
      { key: "cron", labelKey: "features:items.cron", descKey: "features:desc.cron", route: ROUTES.CRON },
      { key: "teams", labelKey: "features:items.teams", descKey: "features:desc.teams", route: ROUTES.TEAMS },
    ],
  },
  {
    key: "conversations",
    labelKey: "features:groups.conversations",
    descKey: "features:groupDesc.conversations",
    features: [
      { key: "sessions", labelKey: "features:items.sessions", descKey: "features:desc.sessions", route: ROUTES.SESSIONS },
      { key: "pending_messages", labelKey: "features:items.pendingMessages", descKey: "features:desc.pendingMessages", route: ROUTES.PENDING_MESSAGES },
      { key: "contacts", labelKey: "features:items.contacts", descKey: "features:desc.contacts", route: ROUTES.CONTACTS },
    ],
  },
  {
    key: "connectivity",
    labelKey: "features:groups.connectivity",
    descKey: "features:groupDesc.connectivity",
    features: [
      { key: "channels", labelKey: "features:items.channels", descKey: "features:desc.channels", route: ROUTES.CHANNELS },
      { key: "nodes", labelKey: "features:items.nodes", descKey: "features:desc.nodes", route: ROUTES.NODES },
    ],
  },
  {
    key: "data",
    labelKey: "features:groups.data",
    descKey: "features:groupDesc.data",
    features: [
      { key: "memory", labelKey: "features:items.memory", descKey: "features:desc.memory", route: ROUTES.MEMORY },
      { key: "knowledge_graph", labelKey: "features:items.knowledgeGraph", descKey: "features:desc.knowledgeGraph", route: ROUTES.KNOWLEDGE_GRAPH },
      { key: "storage", labelKey: "features:items.storage", descKey: "features:desc.storage", route: ROUTES.STORAGE },
    ],
  },
  {
    key: "monitoring",
    labelKey: "features:groups.monitoring",
    descKey: "features:groupDesc.monitoring",
    features: [
      { key: "traces", labelKey: "features:items.traces", descKey: "features:desc.traces", route: ROUTES.TRACES },
      { key: "events", labelKey: "features:items.events", descKey: "features:desc.events", route: ROUTES.EVENTS },
      { key: "delegations", labelKey: "features:items.delegations", descKey: "features:desc.delegations", route: ROUTES.DELEGATIONS },
      { key: "activity", labelKey: "features:items.activity", descKey: "features:desc.activity", route: ROUTES.ACTIVITY },
      { key: "logs", labelKey: "features:items.logs", descKey: "features:desc.logs", route: ROUTES.LOGS },
    ],
  },
  {
    key: "system",
    labelKey: "features:groups.system",
    descKey: "features:groupDesc.system",
    features: [
      { key: "approvals", labelKey: "features:items.approvals", descKey: "features:desc.approvals", route: ROUTES.APPROVALS },
    ],
  },
];

/** Flat lookup: feature key -> route */
export const FEATURE_ROUTE_MAP: Record<string, string> = {};
for (const group of FEATURE_GROUPS) {
  for (const f of group.features) {
    if (f.route) FEATURE_ROUTE_MAP[f.key] = f.route;
  }
}

/** Reverse lookup: route path -> feature key */
export const ROUTE_FEATURE_MAP: Record<string, string> = {};
for (const [key, route] of Object.entries(FEATURE_ROUTE_MAP)) {
  ROUTE_FEATURE_MAP[route] = key;
}
