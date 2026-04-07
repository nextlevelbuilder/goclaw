import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate } from "react-router";
import { Users, Link2 } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { TeamDetailPage } from "./team-detail-page";
import { TeamsListTab } from "./teams-list-tab";
import { TeamLinksTab } from "./links/team-links-tab";

type TabId = "teams" | "links";

const TABS: { id: TabId; icon: typeof Users }[] = [
  { id: "teams", icon: Users },
  { id: "links", icon: Link2 },
];

export function TeamsPage() {
  const { t } = useTranslation("teams");
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<TabId>("teams");

  // Show detail view if route has :id
  if (detailId) {
    return <TeamDetailPage teamId={detailId} onBack={() => navigate("/teams")} />;
  }

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader title={t("title")} description={t("description")} />

      {/* Tab bar */}
      <div className="mt-4 flex items-center gap-1 border-b">
        {TABS.map(({ id, icon: Icon }) => (
          <button
            key={id}
            type="button"
            onClick={() => setActiveTab(id)}
            className={`relative flex items-center gap-1.5 px-3 py-2.5 text-sm font-medium transition-colors focus-visible:outline-none ${
              activeTab === id
                ? "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-primary"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            <Icon className="h-3.5 w-3.5" />
            {t(`tabs.${id}`)}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {activeTab === "teams" && <TeamsListTab onSelectTeam={(id) => navigate(`/teams/${id}`)} />}
      {activeTab === "links" && (
        <div className="mt-6">
          <TeamLinksTab />
        </div>
      )}
    </div>
  );
}
