import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

// --- EN namespaces ---
import enCommon from './locales/en/common.json'
import enChat from './locales/en/chat.json'
import enAgents from './locales/en/agents.json'
import enProviders from './locales/en/providers.json'
import enSkills from './locales/en/skills.json'
import enCron from './locales/en/cron.json'
import enMcp from './locales/en/mcp.json'
import enTools from './locales/en/tools.json'
import enTraces from './locales/en/traces.json'
import enMemory from './locales/en/memory.json'
import enStorage from './locales/en/storage.json'
import enSessions from './locales/en/sessions.json'
import enDesktop from './locales/en/desktop.json'
import enTeams from './locales/en/teams.json'
import enChannels from './locales/en/channels.json'

// --- VI namespaces ---
import viCommon from './locales/vi/common.json'
import viChat from './locales/vi/chat.json'
import viAgents from './locales/vi/agents.json'
import viProviders from './locales/vi/providers.json'
import viSkills from './locales/vi/skills.json'
import viCron from './locales/vi/cron.json'
import viMcp from './locales/vi/mcp.json'
import viTools from './locales/vi/tools.json'
import viTraces from './locales/vi/traces.json'
import viMemory from './locales/vi/memory.json'
import viStorage from './locales/vi/storage.json'
import viSessions from './locales/vi/sessions.json'
import viDesktop from './locales/vi/desktop.json'
import viTeams from './locales/vi/teams.json'
import viChannels from './locales/vi/channels.json'

// --- ZH namespaces ---
import zhCommon from './locales/zh/common.json'
import zhChat from './locales/zh/chat.json'
import zhAgents from './locales/zh/agents.json'
import zhProviders from './locales/zh/providers.json'
import zhSkills from './locales/zh/skills.json'
import zhCron from './locales/zh/cron.json'
import zhMcp from './locales/zh/mcp.json'
import zhTools from './locales/zh/tools.json'
import zhTraces from './locales/zh/traces.json'
import zhMemory from './locales/zh/memory.json'
import zhStorage from './locales/zh/storage.json'
import zhSessions from './locales/zh/sessions.json'
import zhDesktop from './locales/zh/desktop.json'
import zhTeams from './locales/zh/teams.json'
import zhChannels from './locales/zh/channels.json'

const STORAGE_KEY = 'goclaw:language'

function getInitialLanguage(): string {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'en' || stored === 'vi' || stored === 'zh') return stored
  const lang = navigator.language.toLowerCase()
  if (lang.startsWith('vi')) return 'vi'
  if (lang.startsWith('zh')) return 'zh'
  return 'vi'
}

i18n.use(initReactI18next).init({
  resources: {
    en: {
      common: enCommon, chat: enChat, agents: enAgents, providers: enProviders,
      skills: enSkills, cron: enCron, mcp: enMcp, tools: enTools,
      traces: enTraces, memory: enMemory, storage: enStorage, sessions: enSessions,
      desktop: enDesktop, teams: enTeams, channels: enChannels,
    },
    vi: {
      common: viCommon, chat: viChat, agents: viAgents, providers: viProviders,
      skills: viSkills, cron: viCron, mcp: viMcp, tools: viTools,
      traces: viTraces, memory: viMemory, storage: viStorage, sessions: viSessions,
      desktop: viDesktop, teams: viTeams, channels: viChannels,
    },
    zh: {
      common: zhCommon, chat: zhChat, agents: zhAgents, providers: zhProviders,
      skills: zhSkills, cron: zhCron, mcp: zhMcp, tools: zhTools,
      traces: zhTraces, memory: zhMemory, storage: zhStorage, sessions: zhSessions,
      desktop: zhDesktop, teams: zhTeams, channels: zhChannels,
    },
  },
  ns: ['common', 'chat', 'agents', 'providers', 'skills', 'cron', 'mcp', 'tools', 'traces', 'memory', 'storage', 'sessions', 'desktop', 'teams', 'channels'],
  defaultNS: 'common',
  lng: getInitialLanguage(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

// Persist language on change
i18n.on('languageChanged', (lng) => {
  localStorage.setItem(STORAGE_KEY, lng)
  document.documentElement.lang = lng
})

export default i18n
