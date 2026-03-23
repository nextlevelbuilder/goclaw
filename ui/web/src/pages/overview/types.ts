export interface ClientInfo {
  id: string;
  remoteAddr: string;
  userId: string;
  role: string;
  connectedAt: string;
}

export interface HealthPayload {
  status?: string;
  version?: string;
  uptime?: number;
  mode?: string;
  database?: string;
  tools?: number;
  clients?: ClientInfo[];
  currentId?: string;
  latestVersion?: string;
  updateAvailable?: boolean;
  updateUrl?: string;
}

export interface AgentInfo {
  id: string;
  model: string;
  isRunning: boolean;
}

export interface StatusPayload {
  agents?: AgentInfo[];
  agentTotal?: number;
  sessions?: number;
  clients?: number;
}

export interface ChannelStatusEntry {
  enabled: boolean;
  running: boolean;
}

export interface ChannelStatusPayload {
  channels: Record<string, ChannelStatusEntry>;
}

export interface QuotaUsage {
  used: number;
  limit: number;
}

export interface QuotaUsageEntry {
  userId: string;
  hour: QuotaUsage;
  day: QuotaUsage;
  week: QuotaUsage;
}

export interface QuotaUsageResult {
  enabled: boolean;
  requestsToday: number;
  inputTokensToday: number;
  outputTokensToday: number;
  costToday: number;
  uniqueUsersToday: number;
  entries: QuotaUsageEntry[];
}

export interface CronJob {
  id: string;
  name: string;
  enabled: boolean;
  state: {
    nextRunAtMs?: number;
    lastStatus?: string;
  };
}

export interface CronListPayload {
  jobs: CronJob[];
}
