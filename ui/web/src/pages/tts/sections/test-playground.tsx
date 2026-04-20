/**
 * Test Playground section — Step 4 of the TTS configuration flow.
 * Textarea (max 500 chars) + Play/Stop buttons that stream audio from
 * POST /v1/tts/synthesize via the synthesize() hook method.
 * Object URLs are revoked on replace and on unmount to prevent memory leaks.
 */
import { useState, useRef, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Play, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/stores/use-toast-store";
import { AudioTagPicker } from "@/components/audio-tag-picker";
import type { SynthesizeParams } from "../hooks/use-tts-config";

const MAX_CHARS = 500;

/**
 * Pure helper — exported for unit testing.
 * Returns true when the current form state allows audio playback.
 */
export function canPlay({ text, provider }: { text: string; provider: string }): boolean {
  return text.trim().length > 0 && text.length <= MAX_CHARS && provider !== "";
}

/**
 * Pure helper — exported for unit testing.
 * Builds the synthesize API request payload, omitting undefined optional fields.
 */
export function buildSynthesizeRequest(opts: {
  text: string;
  provider: string;
  voiceId?: string;
  modelId?: string;
}): { text: string; provider: string; voice_id?: string; model_id?: string } {
  const req: Record<string, unknown> = { text: opts.text, provider: opts.provider };
  if (opts.voiceId) req.voice_id = opts.voiceId;
  if (opts.modelId) req.model_id = opts.modelId;
  return req as { text: string; provider: string; voice_id?: string; model_id?: string };
}

interface Props {
  provider: string;
  voiceId: string;
  modelId: string;
  synthesize: (params: SynthesizeParams) => Promise<Blob>;
  /** When true, renders the Gemini AudioTagPicker above the textarea so tags
   *  insert directly into the synthesize input. */
  showAudioTags?: boolean;
}

export function TestPlayground({ provider, voiceId, modelId, synthesize, showAudioTags }: Props) {
  const { t } = useTranslation("tts");
  const [text, setText] = useState("");
  const [caretPos, setCaretPos] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [loading, setLoading] = useState(false);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const currentUrlRef = useRef<string | null>(null);

  // Revoke the previous object URL to prevent memory leaks
  const revokeCurrentUrl = useCallback(() => {
    if (currentUrlRef.current) {
      URL.revokeObjectURL(currentUrlRef.current);
      currentUrlRef.current = null;
    }
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      audioRef.current?.pause();
      revokeCurrentUrl();
    };
  }, [revokeCurrentUrl]);

  const handlePlay = async () => {
    if (!text.trim() || !provider) return;
    setLoading(true);
    try {
      const blob = await synthesize({
        text: text.trim(),
        provider,
        voice_id: voiceId || undefined,
        model_id: modelId || undefined,
      });

      revokeCurrentUrl();
      const url = URL.createObjectURL(blob);
      currentUrlRef.current = url;

      if (!audioRef.current) {
        audioRef.current = new Audio();
        audioRef.current.onended = () => setPlaying(false);
        audioRef.current.onerror = () => setPlaying(false);
      }
      audioRef.current.src = url;
      await audioRef.current.play();
      setPlaying(true);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("playground.playbackFailed", "Playback failed"), msg);
    } finally {
      setLoading(false);
    }
  };

  const handleStop = () => {
    audioRef.current?.pause();
    if (audioRef.current) audioRef.current.currentTime = 0;
    setPlaying(false);
  };

  const canPlayNow = canPlay({ text, provider }) && !loading;

  return (
    <Card className="gap-3">
      <CardHeader>
        <CardTitle className="text-base">4. {t("playground.title", "Test Playground")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-1.5">
          <div className="flex items-center justify-between">
            <Label htmlFor="tts-test-text">{t("playground.inputLabel", "Text to synthesize")}</Label>
            <span className="text-xs text-muted-foreground">
              {text.length}/{MAX_CHARS}
            </span>
          </div>
          <Textarea
            id="tts-test-text"
            ref={textareaRef}
            className="text-base md:text-sm min-h-[80px] resize-none"
            placeholder={t("playground.placeholder", "Enter text to hear…")}
            maxLength={MAX_CHARS}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setCaretPos(e.target.selectionStart ?? e.target.value.length);
            }}
            onSelect={(e) => setCaretPos((e.target as HTMLTextAreaElement).selectionStart ?? 0)}
          />
        </div>

        {showAudioTags && (
          <AudioTagPicker
            textValue={text}
            caretPos={caretPos}
            onTextChange={(next) => {
              setText(next);
              // Refocus the textarea so the user can keep typing right after insertion.
              requestAnimationFrame(() => textareaRef.current?.focus());
            }}
          />
        )}

        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            className="h-9 gap-1.5"
            disabled={!canPlayNow || playing}
            onClick={handlePlay}
          >
            <Play className="h-3.5 w-3.5" />
            {loading ? t("playground.synthesizing", "Synthesizing…") : t("playground.play", "Play")}
          </Button>
          {playing && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 gap-1.5"
              onClick={handleStop}
            >
              <Square className="h-3.5 w-3.5" />
              {t("playground.stop", "Stop")}
            </Button>
          )}
        </div>

        {!provider && (
          <p className="text-xs text-muted-foreground">
            {t("playground.requiresProvider", "Select a provider above to enable test playback.")}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
