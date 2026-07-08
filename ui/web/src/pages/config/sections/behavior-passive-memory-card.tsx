import { useEffect, useState } from "react";
import { Brain, Save } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { useHttp } from "@/hooks/use-ws";
import { userFriendlyError } from "@/lib/error-utils";
import { toast } from "@/stores/use-toast-store";

const globalCustomPromptKey = "channel_memory.extraction.custom_prompt";
const queryKey = ["system-configs", globalCustomPromptKey] as const;

export function BehaviorPassiveMemoryCard() {
  const { t } = useTranslation("config");
  const http = useHttp();
  const queryClient = useQueryClient();
  const [value, setValue] = useState("");

  const promptQuery = useQuery({
    queryKey,
    queryFn: async () => {
      const configs = await http.get<Record<string, string>>("/v1/system-configs");
      return configs[globalCustomPromptKey] ?? "";
    },
    staleTime: 30_000,
  });

  useEffect(() => {
    if (promptQuery.data === undefined) return;
    setValue(promptQuery.data);
  }, [promptQuery.data]);

  const save = useMutation({
    mutationFn: () => http.put(`/v1/system-configs/${globalCustomPromptKey}`, { value }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey, exact: true });
      toast.success(t("toast.saved"));
    },
    onError: (err) => {
      toast.error(t("toast.saveFailed"), userFriendlyError(err));
    },
  });

  const dirty = value !== (promptQuery.data ?? "");

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <Brain className="h-4 w-4" />
          {t("behavior.passiveMemoryTitle")}
        </CardTitle>
        <CardDescription>{t("behavior.passiveMemoryDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <Textarea
          value={value}
          onChange={(event) => setValue(event.target.value)}
          className="min-h-[120px] text-base md:text-sm"
          placeholder={t("behavior.passiveMemoryPromptPlaceholder")}
          disabled={promptQuery.isLoading || save.isPending}
        />
        {dirty && (
          <div className="flex justify-end">
            <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {save.isPending ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
