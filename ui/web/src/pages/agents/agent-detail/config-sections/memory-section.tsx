import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { MemoryConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface MemorySectionProps {
  enabled: boolean;
  value: MemoryConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: MemoryConfig) => void;
}

export function MemorySection({ enabled, value, onToggle, onChange }: MemorySectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.memory";
  return (
    <ConfigSection
      title={t(`${s}.title`)}
      description={t(`${s}.description`)}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="flex items-center gap-2">
        <Switch
          checked={value.enabled ?? true}
          onCheckedChange={(v) => onChange({ ...value, enabled: v })}
        />
        <InfoLabel tip="Enable or disable the memory system for this agent. When enabled, the agent can store and recall information across sessions.">{t("configSections.contextPruning.enabled")}</InfoLabel>
      </div>
      <ProviderModelSelect
        provider={value.embedding_provider ?? ""}
        onProviderChange={(v) => onChange({ ...value, embedding_provider: v || undefined })}
        model={value.embedding_model ?? ""}
        onModelChange={(v) => onChange({ ...value, embedding_model: v || undefined })}
        providerLabel={t(`${s}.embeddingProvider`)}
        modelLabel={t(`${s}.embeddingModel`)}
        providerTip="LLM provider used for generating text embeddings. Leave empty to auto-detect. Only providers with embedding enabled are shown."
        modelTip="Embedding model name (e.g. text-embedding-3-small). Must be supported by the provider."
        providerPlaceholder="(auto)"
        modelPlaceholder="text-embedding-3-small"
        allowEmpty
        filterEmbedding
      />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Maximum number of memory entries returned per search query.">{t(`${s}.maxResults`)}</InfoLabel>
          <Input
            type="number"
            placeholder="6"
            value={value.max_results ?? ""}
            onChange={(e) => onChange({ ...value, max_results: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Maximum character length for each memory chunk. Longer content is split into smaller chunks before storing.">{t(`${s}.maxChunkLen`)}</InfoLabel>
          <Input
            type="number"
            placeholder="1000"
            value={value.max_chunk_len ?? ""}
            onChange={(e) => onChange({ ...value, max_chunk_len: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Weight for vector (semantic) similarity in hybrid search scoring. Higher values prioritize meaning over keywords.">{t(`${s}.vectorWeight`)}</InfoLabel>
          <Input
            type="number"
            step="0.1"
            placeholder="0.7"
            value={value.vector_weight ?? ""}
            onChange={(e) => onChange({ ...value, vector_weight: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Weight for text (keyword/BM25) similarity in hybrid search scoring. Higher values prioritize exact keyword matches.">{t(`${s}.textWeight`)}</InfoLabel>
          <Input
            type="number"
            step="0.1"
            placeholder="0.3"
            value={value.text_weight ?? ""}
            onChange={(e) => onChange({ ...value, text_weight: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
    </ConfigSection>
  );
}
