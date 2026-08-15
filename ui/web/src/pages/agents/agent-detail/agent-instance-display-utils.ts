import type { ChannelContact } from "@/types/contact";

interface AgentInstanceLike {
  user_id: string;
  metadata?: Record<string, unknown>;
}

export interface AgentInstanceDisplay {
  label: string;
  instanceId: string;
  channelLabel: string | null;
  chatName: string | null;
  rawUserId: string;
}

type ContactResolver = (id: string) => ChannelContact | null;

const CHANNEL_PREFIX_LABELS: Array<[string, string]> = [
  ["zalo_personal", "Zalo Personal"],
  ["zalo-personal", "Zalo Personal"],
  ["zalo_oa", "Zalo OA"],
  ["zalo-oa", "Zalo OA"],
  ["bitrix24", "Bitrix24"],
  ["bitrix", "Bitrix24"],
  ["telegram", "Telegram"],
  ["tele", "Telegram"],
  ["tg", "Telegram"],
  ["discord", "Discord"],
  ["whatsapp", "WhatsApp"],
  ["facebook", "Facebook"],
  ["pancake", "Pancake"],
  ["feishu", "Feishu"],
  ["lark", "Lark"],
  ["slack", "Slack"],
  ["zalo", "Zalo"],
];

export function getAgentInstanceResolveIds(instances: AgentInstanceLike[]): string[] {
  const ids = new Set<string>();

  for (const instance of instances) {
    const groupTarget = parseGroupTarget(instance.user_id);
    ids.add(groupTarget?.instanceId || instance.user_id);
  }

  return [...ids];
}

export function buildAgentInstanceDisplay(
  instance: AgentInstanceLike,
  resolve: ContactResolver,
): AgentInstanceDisplay {
  const groupTarget = parseGroupTarget(instance.user_id);
  const isGroupInstance = Boolean(groupTarget);
  const contact = resolve(instance.user_id);
  const rawContact = groupTarget ? resolve(groupTarget.instanceId) : null;
  const metadata = instance.metadata ?? {};

  const instanceId = groupTarget?.instanceId || contact?.sender_id || instance.user_id;
  const channelLabel =
    channelLabelFromSource(groupTarget?.channelSource) ??
    channelLabelFromSource(metadata.channel_instance) ??
    channelLabelFromSource(rawContact?.channel_instance) ??
    channelLabelFromSource(contact?.channel_instance) ??
    channelLabelFromSource(metadata.channel_type) ??
    channelLabelFromSource(metadata.channel) ??
    channelLabelFromSource(metadata.platform) ??
    channelLabelFromSource(rawContact?.channel_type) ??
    channelLabelFromSource(contact?.channel_type);

  const metadataTitle =
    clean(metadata.chat_title) ??
    clean(metadata.group_title) ??
    clean(metadata.chat_name);
  const chatName = isGroupInstance
    ? metadataTitle ?? clean(rawContact?.display_name) ?? clean(rawContact?.username)
    : metadataTitle ??
      clean(metadata.display_name) ??
      clean(rawContact?.display_name) ??
      clean(rawContact?.username) ??
      clean(contact?.display_name) ??
      clean(contact?.username);

  return {
    label: formatInstanceLabel(channelLabel, chatName, instanceId),
    instanceId,
    channelLabel,
    chatName,
    rawUserId: instance.user_id,
  };
}

function parseGroupTarget(userID: string): { channelSource: string; instanceId: string } | null {
  if (!userID.startsWith("group:")) return null;

  const rest = userID.slice("group:".length);
  const separatorIndex = rest.indexOf(":");
  if (separatorIndex < 0) return null;

  const channelSource = rest.slice(0, separatorIndex);
  const instanceId = rest.slice(separatorIndex + 1);
  if (!channelSource || !instanceId) return null;

  return { channelSource, instanceId };
}

function channelLabelFromSource(source?: unknown): string | null {
  const value = clean(source)?.toLowerCase();
  if (!value) return null;

  for (const [prefix, label] of CHANNEL_PREFIX_LABELS) {
    if (
      value === prefix ||
      value.startsWith(`${prefix}-`) ||
      value.startsWith(`${prefix}_`) ||
      value.startsWith(`${prefix}:`)
    ) {
      return label;
    }
  }

  return null;
}

function formatInstanceLabel(channelLabel: string | null, chatName: string | null, instanceId: string): string {
  if (channelLabel && chatName) return `${channelLabel} - ${chatName} - ${instanceId}`;
  if (channelLabel) return `${channelLabel} - ${instanceId}`;
  if (chatName) return chatName;
  return instanceId;
}

function clean(value?: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed ? trimmed : null;
}
