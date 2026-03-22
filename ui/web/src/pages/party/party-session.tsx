import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { cn } from "@/lib/utils";
import type { PartyMessage, PartyMode } from "./hooks/use-party";

interface PartySessionProps {
  messages: PartyMessage[];
  getPersonaColor: (key: string) => string;
}

function RoundHeader({ round, mode, t }: { round: number; mode?: PartyMode; t: (k: string, opts?: Record<string, unknown>) => string }) {
  const modeLabel = mode ? t(`mode.${mode}`) : "";
  return (
    <div data-testid="round-header" className="flex items-center gap-2 py-3">
      <div className="h-px flex-1 bg-border" />
      <span className="text-xs font-medium text-muted-foreground">
        {t("round", { n: round })} {modeLabel && `[${modeLabel}]`}
      </span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}

function PersonaMessage({
  message,
  borderColor,
}: {
  message: PartyMessage;
  borderColor: string;
}) {
  const isIntro = message.type === "intro";

  return (
    <div
      data-testid="persona-message"
      className={cn(
        "rounded-md border-l-[3px] bg-muted/30 p-3",
        isIntro && "opacity-80",
      )}
      style={{ borderLeftColor: borderColor }}
    >
      <div className="mb-1 flex items-center gap-1.5">
        <span className="text-sm">{message.personaEmoji}</span>
        <span className="text-sm font-medium">{message.personaName}</span>
        {isIntro && (
          <span className="text-xs text-muted-foreground italic">intro</span>
        )}
      </div>
      <div className="text-sm">
        <MarkdownRenderer content={message.content} />
      </div>
    </div>
  );
}

function ContextMessage({ message }: { message: PartyMessage }) {
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="text-xs text-muted-foreground italic">
        {message.content}
      </span>
    </div>
  );
}

function SummaryMessage({ message }: { message: PartyMessage }) {
  return (
    <div data-testid="summary-message" className="rounded-md border border-primary/20 bg-primary/5 p-4">
      <MarkdownRenderer content={message.content} />
    </div>
  );
}

function ArtifactMessage({ message }: { message: PartyMessage }) {
  return (
    <div className="rounded-md border border-amber-500/20 bg-amber-500/5 p-4">
      <MarkdownRenderer content={message.content} />
    </div>
  );
}

export function PartySession({ messages, getPersonaColor }: PartySessionProps) {
  const { t } = useTranslation("party");
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages.length]);

  return (
    <ScrollArea className="flex-1">
      <div className="space-y-3 p-4">
        {messages.map((msg) => {
          switch (msg.type) {
            case "round_header":
              return (
                <RoundHeader
                  key={msg.id}
                  round={msg.round ?? 0}
                  mode={msg.mode}
                  t={t}
                />
              );
            case "intro":
            case "spoke":
              return (
                <PersonaMessage
                  key={msg.id}
                  message={msg}
                  borderColor={msg.personaKey ? getPersonaColor(msg.personaKey) : "#6b7280"}
                />
              );
            case "context":
              return <ContextMessage key={msg.id} message={msg} />;
            case "summary":
              return <SummaryMessage key={msg.id} message={msg} />;
            case "artifact":
              return <ArtifactMessage key={msg.id} message={msg} />;
            default:
              return null;
          }
        })}
        <div ref={bottomRef} />
      </div>
    </ScrollArea>
  );
}
