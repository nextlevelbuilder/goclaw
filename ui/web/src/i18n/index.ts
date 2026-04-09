import i18n from "i18next";
import { initReactI18next } from "react-i18next";

// --- EN namespaces ---
import enCommon from "./locales/en/common.json";
import enSidebar from "./locales/en/sidebar.json";
import enTopbar from "./locales/en/topbar.json";
import enLogin from "./locales/en/login.json";
import enOverview from "./locales/en/overview.json";
import enChat from "./locales/en/chat.json";
import enAgents from "./locales/en/agents.json";
import enTeams from "./locales/en/teams.json";
import enSessions from "./locales/en/sessions.json";
import enSkills from "./locales/en/skills.json";
import enCron from "./locales/en/cron.json";
import enConfig from "./locales/en/config.json";
import enChannels from "./locales/en/channels.json";
import enProviders from "./locales/en/providers.json";
import enTraces from "./locales/en/traces.json";
import enEvents from "./locales/en/events.json";
import enUsage from "./locales/en/usage.json";
import enApprovals from "./locales/en/approvals.json";
import enNodes from "./locales/en/nodes.json";
import enLogs from "./locales/en/logs.json";
import enTools from "./locales/en/tools.json";
import enMcp from "./locales/en/mcp.json";
import enTts from "./locales/en/tts.json";
import enSetup from "./locales/en/setup.json";
import enMemory from "./locales/en/memory.json";
import enVault from "./locales/en/vault.json";
import enStorage from "./locales/en/storage.json";
import enPendingMessages from "./locales/en/pending-messages.json";
import enContacts from "./locales/en/contacts.json";
import enActivity from "./locales/en/activity.json";
import enApiKeys from "./locales/en/api-keys.json";
import enCliCredentials from "./locales/en/cli-credentials.json";
import enPackages from "./locales/en/packages.json";
import enTenants from "./locales/en/tenants.json";
import enSystemSettings from "./locales/en/system-settings.json";
import enImportExport from "./locales/en/import-export.json";
import enV3Capabilities from "./locales/en/v3-capabilities.json";
import enBackup from "./locales/en/backup.json";

// --- VI namespaces ---
import viCommon from "./locales/vi/common.json";
import viSidebar from "./locales/vi/sidebar.json";
import viTopbar from "./locales/vi/topbar.json";
import viLogin from "./locales/vi/login.json";
import viOverview from "./locales/vi/overview.json";
import viChat from "./locales/vi/chat.json";
import viAgents from "./locales/vi/agents.json";
import viTeams from "./locales/vi/teams.json";
import viSessions from "./locales/vi/sessions.json";
import viSkills from "./locales/vi/skills.json";
import viCron from "./locales/vi/cron.json";
import viConfig from "./locales/vi/config.json";
import viChannels from "./locales/vi/channels.json";
import viProviders from "./locales/vi/providers.json";
import viTraces from "./locales/vi/traces.json";
import viEvents from "./locales/vi/events.json";
import viUsage from "./locales/vi/usage.json";
import viApprovals from "./locales/vi/approvals.json";
import viNodes from "./locales/vi/nodes.json";
import viLogs from "./locales/vi/logs.json";
import viTools from "./locales/vi/tools.json";
import viMcp from "./locales/vi/mcp.json";
import viTts from "./locales/vi/tts.json";
import viSetup from "./locales/vi/setup.json";
import viMemory from "./locales/vi/memory.json";
import viVault from "./locales/vi/vault.json";
import viStorage from "./locales/vi/storage.json";
import viPendingMessages from "./locales/vi/pending-messages.json";
import viContacts from "./locales/vi/contacts.json";
import viActivity from "./locales/vi/activity.json";
import viApiKeys from "./locales/vi/api-keys.json";
import viCliCredentials from "./locales/vi/cli-credentials.json";
import viPackages from "./locales/vi/packages.json";
import viTenants from "./locales/vi/tenants.json";
import viSystemSettings from "./locales/vi/system-settings.json";
import viImportExport from "./locales/vi/import-export.json";
import viV3Capabilities from "./locales/vi/v3-capabilities.json";
import viBackup from "./locales/vi/backup.json";

// --- ZH namespaces ---
import zhCommon from "./locales/zh/common.json";
import zhSidebar from "./locales/zh/sidebar.json";
import zhTopbar from "./locales/zh/topbar.json";
import zhLogin from "./locales/zh/login.json";
import zhOverview from "./locales/zh/overview.json";
import zhChat from "./locales/zh/chat.json";
import zhAgents from "./locales/zh/agents.json";
import zhTeams from "./locales/zh/teams.json";
import zhSessions from "./locales/zh/sessions.json";
import zhSkills from "./locales/zh/skills.json";
import zhCron from "./locales/zh/cron.json";
import zhConfig from "./locales/zh/config.json";
import zhChannels from "./locales/zh/channels.json";
import zhProviders from "./locales/zh/providers.json";
import zhTraces from "./locales/zh/traces.json";
import zhEvents from "./locales/zh/events.json";
import zhUsage from "./locales/zh/usage.json";
import zhApprovals from "./locales/zh/approvals.json";
import zhNodes from "./locales/zh/nodes.json";
import zhLogs from "./locales/zh/logs.json";
import zhTools from "./locales/zh/tools.json";
import zhMcp from "./locales/zh/mcp.json";
import zhTts from "./locales/zh/tts.json";
import zhSetup from "./locales/zh/setup.json";
import zhMemory from "./locales/zh/memory.json";
import zhVault from "./locales/zh/vault.json";
import zhStorage from "./locales/zh/storage.json";
import zhPendingMessages from "./locales/zh/pending-messages.json";
import zhContacts from "./locales/zh/contacts.json";
import zhActivity from "./locales/zh/activity.json";
import zhApiKeys from "./locales/zh/api-keys.json";
import zhCliCredentials from "./locales/zh/cli-credentials.json";
import zhPackages from "./locales/zh/packages.json";
import zhTenants from "./locales/zh/tenants.json";
import zhSystemSettings from "./locales/zh/system-settings.json";
import zhImportExport from "./locales/zh/import-export.json";
import zhV3Capabilities from "./locales/zh/v3-capabilities.json";
import zhBackup from "./locales/zh/backup.json";

const STORAGE_KEY = "goclaw:language";

function getInitialLanguage(): string {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "en" || stored === "vi" || stored === "zh") return stored;
  const lang = navigator.language.toLowerCase();
  if (lang.startsWith("vi")) return "vi";
  if (lang.startsWith("zh")) return "zh";
  return "en";
}

const ns = [
  "common", "sidebar", "topbar", "login", "overview", "chat",
  "agents", "teams", "sessions", "skills", "cron", "config",
  "channels", "providers", "traces", "events",
  "usage", "approvals", "nodes", "logs", "tools", "mcp", "tts",
  "setup", "memory", "vault", "storage", "pending-messages", "contacts", "activity", "api-keys",
  "cli-credentials", "packages", "tenants", "system-settings", "import-export",
  "v3-capabilities",
  "backup",
] as const;

i18n.use(initReactI18next).init({
  resources: {
    en: {
      common: enCommon, sidebar: enSidebar, topbar: enTopbar, login: enLogin,
      overview: enOverview, chat: enChat, agents: enAgents, teams: enTeams,
      sessions: enSessions, skills: enSkills, cron: enCron, config: enConfig,
      channels: enChannels, providers: enProviders, traces: enTraces,
      events: enEvents, usage: enUsage,
      approvals: enApprovals, nodes: enNodes, logs: enLogs, tools: enTools,
      mcp: enMcp, tts: enTts, setup: enSetup, memory: enMemory, vault: enVault, storage: enStorage,
      "pending-messages": enPendingMessages,
      contacts: enContacts, activity: enActivity, "api-keys": enApiKeys,
      "cli-credentials": enCliCredentials,
      packages: enPackages,
      tenants: enTenants,
      "system-settings": enSystemSettings,
      "import-export": enImportExport,
      "v3-capabilities": enV3Capabilities,
      backup: enBackup,
    },
    vi: {
      common: viCommon, sidebar: viSidebar, topbar: viTopbar, login: viLogin,
      overview: viOverview, chat: viChat, agents: viAgents, teams: viTeams,
      sessions: viSessions, skills: viSkills, cron: viCron, config: viConfig,
      channels: viChannels, providers: viProviders, traces: viTraces,
      events: viEvents, usage: viUsage,
      approvals: viApprovals, nodes: viNodes, logs: viLogs, tools: viTools,
      mcp: viMcp, tts: viTts, setup: viSetup, memory: viMemory, vault: viVault, storage: viStorage,
      "pending-messages": viPendingMessages,
      contacts: viContacts, activity: viActivity, "api-keys": viApiKeys,
      "cli-credentials": viCliCredentials,
      packages: viPackages,
      tenants: viTenants,
      "system-settings": viSystemSettings,
      "import-export": viImportExport,
      "v3-capabilities": viV3Capabilities,
      backup: viBackup,
    },
    zh: {
      common: zhCommon, sidebar: zhSidebar, topbar: zhTopbar, login: zhLogin,
      overview: zhOverview, chat: zhChat, agents: zhAgents, teams: zhTeams,
      sessions: zhSessions, skills: zhSkills, cron: zhCron, config: zhConfig,
      channels: zhChannels, providers: zhProviders, traces: zhTraces,
      events: zhEvents, usage: zhUsage,
      approvals: zhApprovals, nodes: zhNodes, logs: zhLogs, tools: zhTools,
      mcp: zhMcp, tts: zhTts, setup: zhSetup, memory: zhMemory, vault: zhVault, storage: zhStorage,
      "pending-messages": zhPendingMessages,
      contacts: zhContacts, activity: zhActivity, "api-keys": zhApiKeys,
      "cli-credentials": zhCliCredentials,
      packages: zhPackages,
      tenants: zhTenants,
      "system-settings": zhSystemSettings,
      "import-export": zhImportExport,
      "v3-capabilities": zhV3Capabilities,
      backup: zhBackup,
    },
  },
  ns: [...ns],
  defaultNS: "common",
  lng: getInitialLanguage(),
  fallbackLng: "en",
  interpolation: { escapeValue: false },
  missingKeyHandler: import.meta.env.DEV
    ? (_lngs, _ns, key) => console.warn(`[i18n] missing: ${key}`)
    : undefined,
});

i18n.on("languageChanged", (lng) => {
  localStorage.setItem(STORAGE_KEY, lng);
  document.documentElement.lang = lng;
});

export default i18n;
