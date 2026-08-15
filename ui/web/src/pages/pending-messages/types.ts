export interface PendingMessageGroup {
  channel_name: string;
  history_key: string;
	parent_history_key?: string;
  group_title?: string;
	parent_group_title?: string;
  message_count: number;
  has_summary: boolean;
  last_activity: string;
}

export function formatPendingGroupLabel(group: Pick<PendingMessageGroup, "history_key" | "parent_history_key" | "group_title" | "parent_group_title">): string {
	const title = group.group_title || group.history_key;
	const parent = group.parent_group_title || group.parent_history_key;
	if (!group.parent_history_key || !parent || parent === title) return title;
	return `${title} / ${parent}`;
}

export interface PendingMessage {
  id: string;
  channel_name: string;
  history_key: string;
  sender: string;
  sender_id: string;
  body: string;
  is_summary: boolean;
  created_at: string;
}
