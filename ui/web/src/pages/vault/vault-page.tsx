import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Search, FileArchive } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useVaultDocuments } from "./hooks/use-vault";
import { VaultDocumentsTable } from "./vault-documents-table";
import { VaultDetailDialog } from "./vault-detail-dialog";
import { VaultSearchDialog } from "./vault-search-dialog";
import type { VaultDocument } from "@/types/vault";

export function VaultPage() {
  const { t } = useTranslation("vault");
  const { agents } = useAgents();

  const [selectedAgent, setSelectedAgent] = useState("");
  const [selectedDoc, setSelectedDoc] = useState<VaultDocument | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);

  const { documents, loading } = useVaultDocuments(selectedAgent, { limit: 50 });

  return (
    <div className="p-3 sm:p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <FileArchive className="h-5 w-5 text-indigo-500" />
          <div>
            <h1 className="text-lg font-semibold">{t("title")}</h1>
            <p className="text-xs text-muted-foreground">{t("description")}</p>
          </div>
        </div>
        <Button
          size="sm" variant="outline"
          onClick={() => setSearchOpen(true)}
          disabled={!selectedAgent}
        >
          <Search className="h-4 w-4 mr-1" />
          {t("search")}
        </Button>
      </div>

      {/* Agent filter */}
      <div className="flex items-center gap-2">
        <label className="text-xs text-muted-foreground shrink-0">{t("filterAgent")}:</label>
        <select
          value={selectedAgent}
          onChange={(e) => setSelectedAgent(e.target.value)}
          className="text-base md:text-sm border rounded px-2 py-1 bg-background"
        >
          <option value="">{t("allAgents")}</option>
          {(agents ?? []).map((a) => (
            <option key={a.id} value={a.id}>
              {a.display_name || a.agent_key}
            </option>
          ))}
        </select>
      </div>

      {/* Documents table */}
      <VaultDocumentsTable
        documents={documents}
        loading={loading}
        onSelect={setSelectedDoc}
      />

      {/* Detail dialog */}
      <VaultDetailDialog
        doc={selectedDoc}
        open={!!selectedDoc}
        onOpenChange={(open) => !open && setSelectedDoc(null)}
      />

      {/* Search dialog */}
      {selectedAgent && (
        <VaultSearchDialog
          agentId={selectedAgent}
          open={searchOpen}
          onOpenChange={setSearchOpen}
          onSelectResult={setSelectedDoc}
        />
      )}
    </div>
  );
}
