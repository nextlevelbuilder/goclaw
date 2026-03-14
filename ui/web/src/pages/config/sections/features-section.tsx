import { useTranslation } from "react-i18next";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import type { FeatureName } from "@/stores/use-features-store";

interface FeatureItem {
    key: FeatureName;
    labelKey: string;
    descKey: string;
}

interface FeatureGroup {
    titleKey: string;
    descKey: string;
    items: FeatureItem[];
}

const FEATURE_GROUPS: FeatureGroup[] = [
    {
        titleKey: "features.groups.core",
        descKey: "features.groups.coreDesc",
        items: [
            { key: "overview", labelKey: "features.overview", descKey: "features.overviewDesc" },
            { key: "chat", labelKey: "features.chat", descKey: "features.chatDesc" },
            { key: "agents", labelKey: "features.agents", descKey: "features.agentsDesc" },
            { key: "agent_teams", labelKey: "features.agentTeams", descKey: "features.agentTeamsDesc" },
        ],
    },
    {
        titleKey: "features.groups.conversations",
        descKey: "features.groups.conversationsDesc",
        items: [
            { key: "sessions", labelKey: "features.sessions", descKey: "features.sessionsDesc" },
            { key: "pending_messages", labelKey: "features.pendingMessages", descKey: "features.pendingMessagesDesc" },
            { key: "contacts", labelKey: "features.contacts", descKey: "features.contactsDesc" },
        ],
    },
    {
        titleKey: "features.groups.connectivity",
        descKey: "features.groups.connectivityDesc",
        items: [
            { key: "channels", labelKey: "features.channels", descKey: "features.channelsDesc" },
            { key: "nodes", labelKey: "features.nodes", descKey: "features.nodesDesc" },
        ],
    },
    {
        titleKey: "features.groups.capabilities",
        descKey: "features.groups.capabilitiesDesc",
        items: [
            { key: "skills", labelKey: "features.skills", descKey: "features.skillsDesc" },
            { key: "builtin_tools", labelKey: "features.builtinTools", descKey: "features.builtinToolsDesc" },
            { key: "mcp_servers", labelKey: "features.mcpServers", descKey: "features.mcpServersDesc" },
            { key: "tts", labelKey: "features.tts", descKey: "features.ttsDesc" },
            { key: "cron", labelKey: "features.cron", descKey: "features.cronDesc" },
        ],
    },
    {
        titleKey: "features.groups.data",
        descKey: "features.groups.dataDesc",
        items: [
            { key: "memory", labelKey: "features.memory", descKey: "features.memoryDesc" },
            { key: "knowledge_graph", labelKey: "features.knowledgeGraph", descKey: "features.knowledgeGraphDesc" },
            { key: "storage", labelKey: "features.storage", descKey: "features.storageDesc" },
        ],
    },
    {
        titleKey: "features.groups.monitoring",
        descKey: "features.groups.monitoringDesc",
        items: [
            { key: "traces", labelKey: "features.traces", descKey: "features.tracesDesc" },
            { key: "events", labelKey: "features.events", descKey: "features.eventsDesc" },
            { key: "delegations", labelKey: "features.delegations", descKey: "features.delegationsDesc" },
            { key: "activity", labelKey: "features.activity", descKey: "features.activityDesc" },
            { key: "logs", labelKey: "features.logs", descKey: "features.logsDesc" },
        ],
    },
    {
        titleKey: "features.groups.system",
        descKey: "features.groups.systemDesc",
        items: [
            { key: "providers", labelKey: "features.providers", descKey: "features.providersDesc" },
            { key: "config", labelKey: "features.config", descKey: "features.configDesc" },
            { key: "approvals", labelKey: "features.approvals", descKey: "features.approvalsDesc" },
        ],
    },
];

interface FeaturesSectionProps {
    data: Record<string, boolean | null | undefined> | undefined;
    onSave: (features: Record<string, boolean | null>) => void;
    saving: boolean;
}

export function FeaturesSection({ data, onSave, saving }: FeaturesSectionProps) {
    const { t } = useTranslation("config");

    const isEnabled = (key: FeatureName): boolean => {
        if (!data) return true;
        const val = data[key];
        return val === undefined || val === null || val === true;
    };

    const handleToggle = (key: FeatureName, checked: boolean) => {
        const updated = { ...data };
        if (checked) {
            // Remove from config (nil = enabled by default)
            delete updated[key];
        } else {
            updated[key] = false;
        }
        onSave(updated as Record<string, boolean | null>);
    };

    return (
        <>
            <Card>
                <CardHeader>
                    <CardTitle>{t("features.title")}</CardTitle>
                    <CardDescription>{t("features.description")}</CardDescription>
                </CardHeader>
            </Card>

            {FEATURE_GROUPS.map((group) => (
                <Card key={group.titleKey}>
                    <CardHeader className="pb-3">
                        <CardTitle className="text-base">{t(group.titleKey)}</CardTitle>
                        <CardDescription className="text-xs">{t(group.descKey)}</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        {group.items.map((item) => (
                            <div key={item.key} className="flex items-center justify-between gap-4">
                                <div className="space-y-0.5">
                                    <Label htmlFor={`feature-${item.key}`} className="text-sm font-medium">
                                        {t(item.labelKey)}
                                    </Label>
                                    <p className="text-xs text-muted-foreground">{t(item.descKey)}</p>
                                </div>
                                <Switch
                                    id={`feature-${item.key}`}
                                    checked={isEnabled(item.key)}
                                    onCheckedChange={(checked) => handleToggle(item.key, checked)}
                                    disabled={saving}
                                />
                            </div>
                        ))}
                    </CardContent>
                </Card>
            ))}
        </>
    );
}
