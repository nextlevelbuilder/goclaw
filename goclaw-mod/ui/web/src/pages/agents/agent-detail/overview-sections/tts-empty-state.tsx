import { VolumeXIcon } from "lucide-react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface Props {
  isOwner: boolean;
}

/**
 * Shown in the agent detail TTS section when no global TTS provider is configured.
 * Owners see a CTA to /tts; non-owners see a hint text only.
 */
export function TtsEmptyState({ isOwner }: Props) {
  const { t } = useTranslation("tts");
  const navigate = useNavigate();

  return (
    <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed p-4 text-center">
      <VolumeXIcon className="size-6 text-muted-foreground" />
      <div className="space-y-0.5">
        <p className="text-sm font-medium">{t("empty_state.title")}</p>
        <p className="text-xs text-muted-foreground">{t("empty_state.description")}</p>
      </div>
      {isOwner ? (
        <Button
          size="sm"
          variant="outline"
          className="mt-1 min-h-[44px] sm:min-h-9"
          onClick={() => navigate("/tts")}
        >
          {t("empty_state.cta")}
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground italic">
          {t("empty_state.non_owner_hint")}
        </p>
      )}
    </div>
  );
}
