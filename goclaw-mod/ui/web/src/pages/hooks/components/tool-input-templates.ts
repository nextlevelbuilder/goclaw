/**
 * Static map of tool_name → JSON skeleton for auto-filling
 * the hook test panel textarea when a tool is selected.
 * Only includes commonly-used properties per tool.
 */
export const TOOL_INPUT_TEMPLATES: Record<string, Record<string, unknown>> = {
  exec: { command: "" },
  web_search: { query: "" },
  web_fetch: { url: "" },
  memory_search: { query: "" },
  read_file: { path: "" },
  write_file: { path: "", content: "" },
  edit: { path: "", old_string: "", new_string: "" },
  list_files: { path: "." },
  delegate: { agent_key: "", task: "" },
  message: { action: "send", message: "" },
  create_image: { prompt: "" },
  tts: { text: "" },
  read_image: { prompt: "", path: "" },
  read_audio: { prompt: "" },
  read_document: { prompt: "" },
  skill_search: { query: "" },
  use_skill: { name: "" },
  heartbeat: { action: "status" },
  cron: { action: "list" },
  vault_search: { query: "" },
  spawn: { task: "" },
  sessions_list: {},
  datetime: {},
};
