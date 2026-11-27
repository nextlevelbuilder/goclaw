import { ChannelFields } from "./channel-fields";
import { groupOverrideSchema } from "./channel-schemas";

export interface TelegramGroupConfigValues {
  group_policy?: string;
  require_mention?: boolean;
  mention_mode?: string;
  enabled?: boolean;
  agent_id?: string;
  allow_from?: string[];
  skills?: string[];
  tools?: string[];
  system_prompt?: string;
}

interface Props {
  config: TelegramGroupConfigValues;
  onChange: (config: TelegramGroupConfigValues) => void;
  idPrefix: string;
}

export function TelegramGroupFields({ config, onChange, idPrefix }: Props) {
  return (
    <ChannelFields
      fields={groupOverrideSchema}
      values={config as Record<string, unknown>}
      onChange={(key, value) => {
        // Empty string (text inputs, which don't auto-strip like textarea/tags)
        // means "inherit", so drop the key entirely to keep saved overrides clean.
        if (value === undefined || value === "") {
          const next = { ...config };
          delete (next as Record<string, unknown>)[key];
          onChange(next);
        } else {
          onChange({ ...config, [key]: value });
        }
      }}
      idPrefix={idPrefix}
      contextValues={config as Record<string, unknown>}
    />
  );
}
