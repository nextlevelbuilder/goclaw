import { useState, useCallback } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { TeamSettingsTab } from "../team-settings-tab";
import { TeamFeaturesModal } from "../team-features-modal";
import { InlineEditText } from "@/components/ui/inline-edit-text";
import { toast } from "@/stores/use-toast-store";
import type { TeamData, TeamMemberData } from "@/types/team";

interface TeamInfoDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  team: TeamData;
  teamId: string;
  members: TeamMemberData[];
  onSaved: () => void;
  onUpdateDescription?: (newDescription: string) => Promise<void>;
}

export function TeamInfoDialog({
  open, onOpenChange, team, teamId, members, onSaved, onUpdateDescription,
}: TeamInfoDialogProps) {
  const { t } = useTranslation("teams");
  const [featuresOpen, setFeaturesOpen] = useState(false);

  const handleDescValidationError = useCallback((err: string) => {
    // Description may be empty (minLength=0), so minLength rarely fires here.
    if (err === "minLength") toast.error(i18next.t("teams:rename.emptyError"));
    else if (err === "stale") toast.error(i18next.t("teams:rename.staleError"));
  }, []);

  // Resolve lead name from members (more reliable than team.lead_display_name which can be empty)
  const leadMember = members.find((m) => m.role === "lead");
  const leadName = leadMember?.display_name || leadMember?.agent_key
    || team.lead_display_name || team.lead_agent_key || "—";

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {team.name}
              <Badge variant={team.status === "active" ? "success" : "secondary"} className="text-2xs">
                {team.status}
              </Badge>
              <button type="button" className="relative inline-flex items-center" onClick={() => setFeaturesOpen(true)}>
                <Badge className="bg-gradient-to-r from-orange-500 to-amber-500 text-2xs px-2 py-0.5 text-white border-0 font-semibold hover:from-orange-600 hover:to-amber-600">
                  v2 Super Team
                </Badge>
              </button>
            </DialogTitle>
          </DialogHeader>

          <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {/* Team overview */}
          <div className="grid grid-cols-1 gap-3 rounded-lg border p-4 text-sm sm:grid-cols-2">
            <div className="sm:col-span-2">
              <span className="text-xs text-muted-foreground">{t("create.description")}</span>
              {onUpdateDescription ? (
                <div className="mt-0.5">
                  <InlineEditText
                    value={team.description ?? ""}
                    onSave={onUpdateDescription}
                    multiline
                    maxLength={2000}
                    minLength={0}
                    placeholder={t("rename.descPlaceholder")}
                    emptyLabel={t("rename.addDescription")}
                    ariaLabel={t("rename.descAria")}
                    wrapperClassName="w-full"
                    className="w-full"
                    onValidationError={handleDescValidationError}
                  />
                </div>
              ) : (
                team.description && <p className="mt-0.5">{team.description}</p>
              )}
            </div>
            <div>
              <span className="text-xs text-muted-foreground">{t("detail.lead")}</span>
              <p className="mt-0.5 font-medium">{leadName}</p>
            </div>
            <div>
              <span className="text-xs text-muted-foreground">{t("members.title")}</span>
              <p className="mt-0.5 font-medium">{t("detail.memberCountPlural", { count: members.length })}</p>
            </div>
          </div>

          {/* Settings form */}
          <TeamSettingsTab teamId={teamId} team={team} onSaved={() => { onSaved(); onOpenChange(false); }} />
          </div>
        </DialogContent>
      </Dialog>
      <TeamFeaturesModal open={featuresOpen} onOpenChange={setFeaturesOpen} />
    </>
  );
}
