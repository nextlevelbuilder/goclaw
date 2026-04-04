export interface MergeContactsRequest {
  contact_ids: string[];
  tenant_user_id?: string;
  create_user?: { user_id: string; display_name?: string };
}

export interface MergeContactsResponse {
  merged_id: string;
  merged_count: number;
}

export interface ChannelContact {
  id: string;
  channel_type: string;
  channel_instance?: string;
  sender_id: string;
  user_id?: string;
  display_name?: string;
  username?: string;
  avatar_url?: string;
  peer_kind?: string;
  contact_type: string; // "user", "group", or "topic"
  thread_id?: string;
  thread_type?: string;
  merged_id?: string;
  first_seen_at: string;
  last_seen_at: string;
}
