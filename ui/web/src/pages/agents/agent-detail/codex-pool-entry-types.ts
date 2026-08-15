import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";
import type { CodexPoolRecentRequest } from "./hooks/use-codex-pool-activity";

export interface CodexPoolEntry {
  name: string;
  label: string;
  availability: ChatGPTOAuthAvailability;
  role: "preferred" | "extra";
  requestCount: number;
  directSelectionCount: number;
  failoverServeCount: number;
  successCount: number;
  failureCount: number;
  consecutiveFailures: number;
  successRate: number;
  healthScore: number;
  healthState: "healthy" | "degraded" | "critical" | "idle";
  lastSelectedAt?: string;
  lastFailoverAt?: string;
  lastUsedAt?: string;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  providerHref?: string;
  quota?: ChatGPTOAuthProviderQuota | null;
}

export interface CodexPoolActivityPanelProps {
  entries: CodexPoolEntry[];
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  recentRequests: CodexPoolRecentRequest[];
  statsSampleSize: number;
  fetching: boolean;
  showProviderLinks?: boolean;
  onRefresh: () => void;
  className?: string;
}

export interface CodexPoolRecentRequestsPanelProps {
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  className?: string;
}

export interface CodexPoolRecentRequestsListProps {
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  compact?: boolean;
  className?: string;
}
