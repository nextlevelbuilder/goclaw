import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { PipelineTab } from "./pipeline-tab";
import { MemoryTab } from "./memory-tab";
import { KnowledgeTab } from "./knowledge-tab";
import { OrchestrationTab } from "./orchestration-tab";

interface V3CapabilitiesModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function V3CapabilitiesModal({
  open,
  onOpenChange,
}: V3CapabilitiesModalProps) {
  const { t } = useTranslation("v3-capabilities");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl max-h-[85dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("title")}</DialogTitle>
          <DialogDescription>{t("subtitle")}</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="pipeline">
          <TabsList className="w-full">
            <TabsTrigger value="pipeline">{t("tabs.pipeline")}</TabsTrigger>
            <TabsTrigger value="memory">{t("tabs.memory")}</TabsTrigger>
            <TabsTrigger value="knowledge">{t("tabs.knowledge")}</TabsTrigger>
            <TabsTrigger value="orchestration">
              {t("tabs.orchestration")}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="pipeline">
            <PipelineTab />
          </TabsContent>
          <TabsContent value="memory">
            <MemoryTab />
          </TabsContent>
          <TabsContent value="knowledge">
            <KnowledgeTab />
          </TabsContent>
          <TabsContent value="orchestration">
            <OrchestrationTab />
          </TabsContent>
        </Tabs>

        <p className="text-xs text-muted-foreground">{t("note")}</p>
      </DialogContent>
    </Dialog>
  );
}
