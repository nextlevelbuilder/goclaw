export type ParamField = {
  key: string;
  label: string;
  type: "select" | "toggle" | "number" | "text";
  options?: { value: string; label: string }[];
  default?: unknown;
  min?: number;
  max?: number;
  step?: number;
  description?: string;
};

export const MEDIA_PARAMS_SCHEMA: Record<string, Record<string, ParamField[]>> = {
  create_image: {
    minimax_native: [],
    bailian: [
      {
        key: "size",
        label: "Size",
        type: "select",
        default: "1024*1024",
        options: [
          { value: "1024*1024", label: "1K" },
          { value: "2048*2048", label: "2K" },
        ],
      },
      { key: "prompt_extend", label: "Prompt Extend", type: "toggle", default: true },
    ],
    dashscope: [
      {
        key: "size",
        label: "Size",
        type: "select",
        default: "1024*1024",
        options: [
          { value: "1024*1024", label: "1K" },
          { value: "2048*2048", label: "2K" },
        ],
      },
      { key: "prompt_extend", label: "Prompt Extend", type: "toggle", default: true },
    ],
  },
  create_video: {
    minimax_native: [
      {
        key: "resolution",
        label: "Resolution",
        type: "select",
        default: "720P",
        options: [
          { value: "720P", label: "720P" },
          { value: "768P", label: "768P" },
          { value: "1080P", label: "1080P" },
        ],
      },
      { key: "prompt_optimizer", label: "Prompt Optimizer", type: "toggle", default: true },
      { key: "fast_pretreatment", label: "Fast Mode", type: "toggle", default: false },
    ],
    gemini_native: [
      {
        key: "resolution",
        label: "Resolution",
        type: "select",
        default: "720p",
        options: [
          { value: "720p", label: "720p (50% cheaper)" },
          { value: "1080p", label: "1080p" },
        ],
      },
      {
        key: "generate_audio",
        label: "Generate Audio",
        type: "toggle",
        default: true,
        description: "Auto-generate synchronized audio",
      },
      {
        key: "person_generation",
        label: "Person Generation",
        type: "select",
        default: "allow_all",
        options: [
          { value: "allow_all", label: "Allow All" },
          { value: "dont_allow", label: "Don't Allow" },
        ],
      },
    ],
  },
  create_audio: {
    minimax_native: [
      { key: "lyrics_optimizer", label: "Auto Lyrics", type: "toggle", default: false },
      {
        key: "sample_rate",
        label: "Sample Rate",
        type: "select",
        default: "44100",
        options: [
          { value: "44100", label: "44.1kHz" },
          { value: "48000", label: "48kHz" },
        ],
      },
      {
        key: "bitrate",
        label: "Bitrate",
        type: "select",
        default: "256000",
        options: [
          { value: "32000", label: "32kbps" },
          { value: "64000", label: "64kbps" },
          { value: "128000", label: "128kbps" },
          { value: "256000", label: "256kbps" },
        ],
      },
    ],
    suno: [
      {
        key: "style",
        label: "Style",
        type: "text",
        default: "",
        description: "e.g. Pop, Classical, Jazz",
      },
      {
        key: "vocal_gender",
        label: "Vocal Gender",
        type: "select",
        default: "",
        options: [
          { value: "", label: "Any" },
          { value: "m", label: "Male" },
          { value: "f", label: "Female" },
        ],
      },
    ],
  },
};

export const MEDIA_TOOLS = new Set([
  "read_image",
  "read_document",
  "read_audio",
  "read_video",
  "create_image",
  "create_video",
  "create_audio",
]);
