import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { PartyPopper, Plus } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { cn } from "@/lib/utils";
import { useParty } from "./hooks/use-party";
import { PartyStartDialog } from "./party-start-dialog";
import { PartySession } from "./party-session";
import { PersonaSidebar } from "./persona-sidebar";
import { PartyControls } from "./party-controls";

export function PartyPage() {
  const { t } = useTranslation("party");
  const {
    sessions,
    activeSessionId,
    messages,
    personas,
    thinkingPersonas,
    mode,
    status,
    loading,
    listSessions,
    startParty,
    runRound,
    askQuestion,
    getSummary,
    exitParty,
    selectSession,
    getPersonaColor,
  } = useParty();

  const [createOpen, setCreateOpen] = useState(false);
  const showSkeleton = useDeferredLoading(loading && sessions.length === 0);

  useEffect(() => {
    listSessions();
  }, [listSessions]);

  const hasActiveSession = activeSessionId !== null && status !== "idle";

  return (
    <div className="flex h-full flex-col">
      {/* Header area */}
      <div className="shrink-0 p-4 sm:p-6 pb-0 sm:pb-0">
        <PageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <Button onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-4 w-4" /> {t("newParty")}
            </Button>
          }
        />
      </div>

      {/* Main content */}
      <div className="flex flex-1 min-h-0 mt-4">
        {/* Session list panel */}
        <div className="w-64 shrink-0 border-r">
          <div className="border-b px-3 py-2">
            <h3 className="text-xs font-semibold uppercase text-muted-foreground tracking-wider">
              Sessions
            </h3>
          </div>
          <ScrollArea className="h-full">
            <div className="space-y-1 p-2">
              {showSkeleton ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <CardSkeleton key={i} />
                ))
              ) : sessions.length === 0 ? (
                <p className="px-2 py-4 text-center text-xs text-muted-foreground">
                  {t("noSessions")}
                </p>
              ) : (
                sessions.map((session) => (
                  <button
                    key={session.id}
                    data-testid="session-item"
                    type="button"
                    onClick={() => selectSession(session)}
                    className={cn(
                      "w-full rounded-md px-3 py-2 text-left transition-colors cursor-pointer",
                      activeSessionId === session.id
                        ? "bg-primary/10 text-primary"
                        : "hover:bg-muted/50",
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium">
                        {session.topic}
                      </span>
                      <Badge
                        variant={session.status === "active" ? "success" : "secondary"}
                        className="shrink-0 text-[10px]"
                      >
                        {session.status}
                      </Badge>
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted-foreground">
                      <span>{t("round", { n: session.round })}</span>
                      <span>{session.personas.length} personas</span>
                    </div>
                  </button>
                ))
              )}
            </div>
          </ScrollArea>
        </div>

        {/* Active session area */}
        {hasActiveSession ? (
          <div className="flex flex-1 min-w-0 flex-col">
            {/* Chat + persona sidebar */}
            <div className="flex flex-1 min-h-0">
              {/* Chat messages */}
              <PartySession
                messages={messages}
                getPersonaColor={getPersonaColor}
              />

              {/* Right sidebar with persona list */}
              <PersonaSidebar
                personas={personas}
                thinkingPersonas={thinkingPersonas}
              />
            </div>

            {/* Bottom controls */}
            <PartyControls
              sessionId={activeSessionId!}
              currentMode={mode}
              status={status}
              onRunRound={runRound}
              onAskQuestion={askQuestion}
              onGetSummary={getSummary}
              onExit={exitParty}
            />
          </div>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState
              icon={PartyPopper}
              title={t("noSessions")}
              description={t("description")}
              action={
                <Button onClick={() => setCreateOpen(true)} className="gap-1">
                  <Plus className="h-4 w-4" /> {t("newParty")}
                </Button>
              }
            />
          </div>
        )}
      </div>

      {/* Start dialog */}
      <PartyStartDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onStart={startParty}
      />
    </div>
  );
}
