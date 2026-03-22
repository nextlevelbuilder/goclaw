import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import type { PersonaInfo } from "./hooks/use-party";

interface PersonaSidebarProps {
  personas: PersonaInfo[];
  thinkingPersonas: Set<string>;
}

export function PersonaSidebar({ personas, thinkingPersonas }: PersonaSidebarProps) {
  const { t } = useTranslation("party");

  if (personas.length === 0) return null;

  return (
    <div className="w-56 shrink-0 border-l bg-muted/20">
      <div className="border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase text-muted-foreground tracking-wider">
          Personas
        </h3>
      </div>
      <div className="space-y-1 p-2">
        {personas.map((persona) => {
          const isThinking = thinkingPersonas.has(persona.key);
          return (
            <div
              key={persona.key}
              data-testid="persona-item"
              className="flex items-center gap-2 rounded-md px-2 py-1.5"
            >
              {/* Status indicator */}
              <span
                className={cn(
                  "inline-block h-2 w-2 shrink-0 rounded-full",
                  isThinking
                    ? "animate-pulse bg-emerald-500"
                    : "bg-muted-foreground/30",
                )}
              />
              {/* Persona info */}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1">
                  <span className="text-sm">{persona.emoji}</span>
                  <span className="truncate text-xs font-medium">
                    {persona.name}
                  </span>
                </div>
                <p className="truncate text-[10px] text-muted-foreground">
                  {persona.role}
                </p>
              </div>
              {/* Status label */}
              {isThinking && (
                <span className="text-[10px] text-emerald-600 dark:text-emerald-400">
                  {t("status.thinking")}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
