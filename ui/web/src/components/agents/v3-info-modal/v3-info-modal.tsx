import { useTranslation } from "react-i18next";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { V3InfoTabCore } from "./v3-info-tab-core";
import { V3InfoTabMemory } from "./v3-info-tab-memory";
import { V3InfoTabOrchestration } from "./v3-info-tab-orchestration";

interface V3InfoModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function V3InfoModal({ open, onOpenChange }: V3InfoModalProps) {
  const { t } = useTranslation("agents");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl sm:max-h-[85dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("v3Info.title")}
            <Badge className="bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">v3</Badge>
          </DialogTitle>
          <DialogDescription>{t("v3Info.subtitle")}</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="core">
          <TabsList className="w-full">
            <TabsTrigger value="core">{t("v3Info.tabs.core")}</TabsTrigger>
            <TabsTrigger value="memory">{t("v3Info.tabs.memory")}</TabsTrigger>
            <TabsTrigger value="orchestration">{t("v3Info.tabs.orchestration")}</TabsTrigger>
          </TabsList>

          <TabsContent value="core"><V3InfoTabCore /></TabsContent>
          <TabsContent value="memory"><V3InfoTabMemory /></TabsContent>
          <TabsContent value="orchestration"><V3InfoTabOrchestration /></TabsContent>
        </Tabs>

        <p className="text-xs text-muted-foreground">{t("v3Info.note")}</p>
      </DialogContent>
    </Dialog>
  );
}
