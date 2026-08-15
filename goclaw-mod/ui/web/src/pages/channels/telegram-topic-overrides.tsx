import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import { TelegramGroupFields, type TelegramGroupConfigValues } from "./telegram-group-fields";

export type TelegramTopicConfigValues = TelegramGroupConfigValues;

interface Props {
  topics: Record<string, TelegramTopicConfigValues>;
  onChange: (topics: Record<string, TelegramTopicConfigValues>) => void;
}

export function TelegramTopicOverrides({ topics, onChange }: Props) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [newTopicId, setNewTopicId] = useState("");

  const topicIds = Object.keys(topics);

  const addTopic = () => {
    const id = newTopicId.trim();
    if (!id || topics[id]) return;
    onChange({ ...topics, [id]: {} });
    setExpanded((prev) => ({ ...prev, [id]: true }));
    setNewTopicId("");
  };

  const removeTopic = (id: string) => {
    const next = { ...topics };
    delete next[id];
    onChange(next);
  };

  const updateTopic = (id: string, config: TelegramTopicConfigValues) => {
    onChange({ ...topics, [id]: config });
  };

  const toggle = (id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  return (
    <div className="space-y-2">
      <Label className="text-xs font-medium text-muted-foreground">{t("groupOverrides.topicOverrides")}</Label>

      {topicIds.map((id) => (
        <div key={id} className="rounded-md border border-dashed p-2 space-y-2">
          <div className="flex items-center justify-between">
            <button
              type="button"
              className="flex items-center gap-1 text-sm font-medium hover:underline"
              onClick={() => toggle(id)}
            >
              {expanded[id] ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
              {t("groupOverrides.topicLabel", { id })}
            </button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive"
              onClick={() => removeTopic(id)}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>

          {expanded[id] && (
            <div className="pl-4">
              <TelegramGroupFields
                config={topics[id] ?? {}}
                onChange={(cfg) => updateTopic(id, cfg)}
                idPrefix={`topic-${id}`}
              />
            </div>
          )}
        </div>
      ))}

      <div className="flex items-center gap-2">
        <Input
          value={newTopicId}
          onChange={(e) => setNewTopicId(e.target.value.replace(/\D/g, ""))}
          placeholder={t("groupOverrides.addTopicPlaceholder")}
          className="h-8 w-40 text-sm"
          onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addTopic())}
        />
        <Button type="button" variant="outline" size="sm" className="h-8" onClick={addTopic} disabled={!newTopicId.trim()}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t("groupOverrides.addTopic")}
        </Button>
      </div>
    </div>
  );
}
