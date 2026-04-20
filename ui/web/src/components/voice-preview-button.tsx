import { useRef, useState } from "react";
import { PlayIcon, StopCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { toast } from "@/stores/use-toast-store";
import { useRefreshVoices } from "@/api/voices";

interface Props {
  previewUrl?: string;
  voiceName: string;
}

// Singleton audio element — only one preview plays at a time across all instances.
let globalAudio: HTMLAudioElement | null = null;
let globalStop: (() => void) | null = null;

export function VoicePreviewButton({ previewUrl, voiceName }: Props) {
  const { t } = useTranslation("tts");
  const [playing, setPlaying] = useState(false);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const { mutate: refreshVoices } = useRefreshVoices();

  const stop = () => {
    if (audioRef.current) {
      audioRef.current.pause();
      audioRef.current.src = "";
      audioRef.current = null;
    }
    setPlaying(false);
  };

  const handlePlay = () => {
    if (!previewUrl) return;

    // Stop any other preview that is currently playing.
    if (globalAudio && globalAudio !== audioRef.current) {
      globalAudio.pause();
      globalAudio.src = "";
      globalStop?.();
    }

    if (playing) {
      stop();
      globalAudio = null;
      globalStop = null;
      return;
    }

    const audio = new Audio(previewUrl);
    audioRef.current = audio;
    globalAudio = audio;
    globalStop = stop;

    audio.play().catch(() => {
      // Preview URL may have expired — refresh the voice list.
      toast.warning(t("voice_preview_error"));
      refreshVoices();
      stop();
      globalAudio = null;
      globalStop = null;
    });

    audio.onended = () => {
      stop();
      if (globalAudio === audio) {
        globalAudio = null;
        globalStop = null;
      }
    };

    audio.onerror = () => {
      toast.warning(t("voice_preview_error"));
      refreshVoices();
      stop();
      if (globalAudio === audio) {
        globalAudio = null;
        globalStop = null;
      }
    };

    setPlaying(true);
  };

  if (!previewUrl) return null;

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      title={playing ? t("voice_stop_preview") : t("voice_preview", { name: voiceName })}
      onClick={handlePlay}
      className="shrink-0"
    >
      {playing ? (
        <StopCircleIcon className="size-3" />
      ) : (
        <PlayIcon className="size-3" />
      )}
    </Button>
  );
}
