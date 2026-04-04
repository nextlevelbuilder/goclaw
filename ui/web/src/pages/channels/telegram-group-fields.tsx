import { ChannelFields } from "./channel-fields";
import { groupOverrideSchema } from "./channel-schemas";

export interface TelegramGroupConfigValues {
  group_policy?: string;
  require_mention?: boolean;
  mention_mode?: string;
  enabled?: boolean;
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
      onChange={(key, value) => onChange({ ...config, [key]: value })}
      idPrefix={idPrefix}
      contextValues={config as Record<string, unknown>}
    />
  );
}
