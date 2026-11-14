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
import enHooks from "./locales/en/hooks.json";
import enWebhooks from "./locales/en/webhooks.json";
import enWorkstations from "./locales/en/workstations.json";

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
import viHooks from "./locales/vi/hooks.json";
import viWebhooks from "./locales/vi/webhooks.json";
import viWorkstations from "./locales/vi/workstations.json";

// --- KO namespaces ---
import koCommon from "./locales/ko/common.json";
import koSidebar from "./locales/ko/sidebar.json";
import koTopbar from "./locales/ko/topbar.json";
import koLogin from "./locales/ko/login.json";
import koOverview from "./locales/ko/overview.json";
import koChat from "./locales/ko/chat.json";
import koAgents from "./locales/ko/agents.json";
import koTeams from "./locales/ko/teams.json";
import koSessions from "./locales/ko/sessions.json";
import koSkills from "./locales/ko/skills.json";
import koCron from "./locales/ko/cron.json";
import koConfig from "./locales/ko/config.json";
import koChannels from "./locales/ko/channels.json";
import koProviders from "./locales/ko/providers.json";
import koTraces from "./locales/ko/traces.json";
import koEvents from "./locales/ko/events.json";
import koUsage from "./locales/ko/usage.json";
import koApprovals from "./locales/ko/approvals.json";
import koNodes from "./locales/ko/nodes.json";
import koLogs from "./locales/ko/logs.json";
import koTools from "./locales/ko/tools.json";
import koMcp from "./locales/ko/mcp.json";
import koTts from "./locales/ko/tts.json";
import koSetup from "./locales/ko/setup.json";
import koMemory from "./locales/ko/memory.json";
import koVault from "./locales/ko/vault.json";
import koStorage from "./locales/ko/storage.json";
import koPendingMessages from "./locales/ko/pending-messages.json";
import koContacts from "./locales/ko/contacts.json";
import koActivity from "./locales/ko/activity.json";
import koApiKeys from "./locales/ko/api-keys.json";
import koCliCredentials from "./locales/ko/cli-credentials.json";
import koPackages from "./locales/ko/packages.json";
import koTenants from "./locales/ko/tenants.json";
import koSystemSettings from "./locales/ko/system-settings.json";
import koImportExport from "./locales/ko/import-export.json";
import koV3Capabilities from "./locales/ko/v3-capabilities.json";
import koBackup from "./locales/ko/backup.json";

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
import zhHooks from "./locales/zh/hooks.json";
import zhWebhooks from "./locales/zh/webhooks.json";
import zhWorkstations from "./locales/zh/workstations.json";

// --- RU namespaces ---
import ruCommon from "./locales/ru/common.json";
import ruSidebar from "./locales/ru/sidebar.json";
import ruTopbar from "./locales/ru/topbar.json";
import ruLogin from "./locales/ru/login.json";
import ruOverview from "./locales/ru/overview.json";
import ruChat from "./locales/ru/chat.json";
import ruAgents from "./locales/ru/agents.json";
import ruTeams from "./locales/ru/teams.json";
import ruSessions from "./locales/ru/sessions.json";
import ruSkills from "./locales/ru/skills.json";
import ruCron from "./locales/ru/cron.json";
import ruConfig from "./locales/ru/config.json";
import ruChannels from "./locales/ru/channels.json";
import ruProviders from "./locales/ru/providers.json";
import ruTraces from "./locales/ru/traces.json";
import ruEvents from "./locales/ru/events.json";
import ruUsage from "./locales/ru/usage.json";
import ruApprovals from "./locales/ru/approvals.json";
import ruNodes from "./locales/ru/nodes.json";
import ruLogs from "./locales/ru/logs.json";
import ruTools from "./locales/ru/tools.json";
import ruMcp from "./locales/ru/mcp.json";
import ruTts from "./locales/ru/tts.json";
import ruSetup from "./locales/ru/setup.json";
import ruMemory from "./locales/ru/memory.json";
import ruVault from "./locales/ru/vault.json";
import ruStorage from "./locales/ru/storage.json";
import ruPendingMessages from "./locales/ru/pending-messages.json";
import ruContacts from "./locales/ru/contacts.json";
import ruActivity from "./locales/ru/activity.json";
import ruApiKeys from "./locales/ru/api-keys.json";
import ruCliCredentials from "./locales/ru/cli-credentials.json";
import ruPackages from "./locales/ru/packages.json";
import ruTenants from "./locales/ru/tenants.json";
import ruSystemSettings from "./locales/ru/system-settings.json";
import ruImportExport from "./locales/ru/import-export.json";
import ruV3Capabilities from "./locales/ru/v3-capabilities.json";
import ruBackup from "./locales/ru/backup.json";
import ruHooks from "./locales/ru/hooks.json";
import ruWebhooks from "./locales/ru/webhooks.json";
import ruWorkstations from "./locales/ru/workstations.json";

// --- ES namespaces ---
import esCommon from "./locales/es/common.json";
import esSidebar from "./locales/es/sidebar.json";
import esTopbar from "./locales/es/topbar.json";
import esLogin from "./locales/es/login.json";
import esOverview from "./locales/es/overview.json";
import esChat from "./locales/es/chat.json";
import esAgents from "./locales/es/agents.json";
import esTeams from "./locales/es/teams.json";
import esSessions from "./locales/es/sessions.json";
import esSkills from "./locales/es/skills.json";
import esCron from "./locales/es/cron.json";
import esConfig from "./locales/es/config.json";
import esChannels from "./locales/es/channels.json";
import esProviders from "./locales/es/providers.json";
import esTraces from "./locales/es/traces.json";
import esEvents from "./locales/es/events.json";
import esUsage from "./locales/es/usage.json";
import esApprovals from "./locales/es/approvals.json";
import esNodes from "./locales/es/nodes.json";
import esLogs from "./locales/es/logs.json";
import esTools from "./locales/es/tools.json";
import esMcp from "./locales/es/mcp.json";
import esTts from "./locales/es/tts.json";
import esSetup from "./locales/es/setup.json";
import esMemory from "./locales/es/memory.json";
import esVault from "./locales/es/vault.json";
import esStorage from "./locales/es/storage.json";
import esPendingMessages from "./locales/es/pending-messages.json";
import esContacts from "./locales/es/contacts.json";
import esActivity from "./locales/es/activity.json";
import esApiKeys from "./locales/es/api-keys.json";
import esCliCredentials from "./locales/es/cli-credentials.json";
import esPackages from "./locales/es/packages.json";
import esTenants from "./locales/es/tenants.json";
import esSystemSettings from "./locales/es/system-settings.json";
import esImportExport from "./locales/es/import-export.json";
import esV3Capabilities from "./locales/es/v3-capabilities.json";
import esBackup from "./locales/es/backup.json";
import esHooks from "./locales/es/hooks.json";
import esWebhooks from "./locales/es/webhooks.json";
import esWorkstations from "./locales/es/workstations.json";

const STORAGE_KEY = "goclaw:language";

function getInitialLanguage(): string {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "en" || stored === "vi" || stored === "zh" || stored === "ko" || stored === "ru" || stored === "es") return stored;
  const lang = navigator.language.toLowerCase();
  if (lang.startsWith("vi")) return "vi";
  if (lang.startsWith("zh")) return "zh";
  if (lang.startsWith("ko")) return "ko";
  if (lang.startsWith("ru")) return "ru";
  if (lang.startsWith("es")) return "es";
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
  "hooks",
  "webhooks",
  "workstations",
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
      hooks: enHooks,
      webhooks: enWebhooks,
      workstations: enWorkstations,
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
      hooks: viHooks,
      webhooks: viWebhooks,
      workstations: viWorkstations,
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
      hooks: zhHooks,
      webhooks: zhWebhooks,
      workstations: zhWorkstations,
    },
    ko: {
      common: koCommon, sidebar: koSidebar, topbar: koTopbar, login: koLogin,
      overview: koOverview, chat: koChat, agents: koAgents, teams: koTeams,
      sessions: koSessions, skills: koSkills, cron: koCron, config: koConfig,
      channels: koChannels, providers: koProviders, traces: koTraces,
      events: koEvents, usage: koUsage,
      approvals: koApprovals, nodes: koNodes, logs: koLogs, tools: koTools,
      mcp: koMcp, tts: koTts, setup: koSetup, memory: koMemory, vault: koVault, storage: koStorage,
      "pending-messages": koPendingMessages,
      contacts: koContacts, activity: koActivity, "api-keys": koApiKeys,
      "cli-credentials": koCliCredentials,
      packages: koPackages,
      tenants: koTenants,
      "system-settings": koSystemSettings,
      "import-export": koImportExport,
      "v3-capabilities": koV3Capabilities,
      backup: koBackup,
    },
    ru: {
      common: ruCommon, sidebar: ruSidebar, topbar: ruTopbar, login: ruLogin,
      overview: ruOverview, chat: ruChat, agents: ruAgents, teams: ruTeams,
      sessions: ruSessions, skills: ruSkills, cron: ruCron, config: ruConfig,
      channels: ruChannels, providers: ruProviders, traces: ruTraces,
      events: ruEvents, usage: ruUsage,
      approvals: ruApprovals, nodes: ruNodes, logs: ruLogs, tools: ruTools,
      mcp: ruMcp, tts: ruTts, setup: ruSetup, memory: ruMemory, vault: ruVault, storage: ruStorage,
      "pending-messages": ruPendingMessages,
      contacts: ruContacts, activity: ruActivity, "api-keys": ruApiKeys,
      "cli-credentials": ruCliCredentials,
      packages: ruPackages,
      tenants: ruTenants,
      "system-settings": ruSystemSettings,
      "import-export": ruImportExport,
      "v3-capabilities": ruV3Capabilities,
      backup: ruBackup,
      hooks: ruHooks,
      webhooks: ruWebhooks,
      workstations: ruWorkstations,
    },
    es: {
      common: esCommon,
      sidebar: esSidebar,
      topbar: esTopbar,
      login: esLogin,
      overview: esOverview,
      chat: esChat,
      agents: esAgents,
      teams: esTeams,
      sessions: esSessions,
      skills: esSkills,
      cron: esCron,
      config: esConfig,
      channels: esChannels,
      providers: esProviders,
      traces: esTraces,
      events: esEvents,
      usage: esUsage,
      approvals: esApprovals,
      nodes: esNodes,
      logs: esLogs,
      tools: esTools,
      mcp: esMcp,
      tts: esTts,
      setup: esSetup,
      memory: esMemory,
      vault: esVault,
      storage: esStorage,
      "pending-messages": esPendingMessages,
      contacts: esContacts,
      activity: esActivity,
      "api-keys": esApiKeys,
      "cli-credentials": esCliCredentials,
      packages: esPackages,
      tenants: esTenants,
      "system-settings": esSystemSettings,
      "import-export": esImportExport,
      "v3-capabilities": esV3Capabilities,
      backup: esBackup,
      hooks: esHooks,
      webhooks: esWebhooks,
      workstations: esWorkstations,
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
