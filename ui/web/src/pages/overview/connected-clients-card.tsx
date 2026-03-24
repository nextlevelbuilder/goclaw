import { Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { ClientInfo } from "./types";
import { formatClientTime } from "./hooks/use-live-uptime";

export function ConnectedClientsCard({
  clients,
  currentId,
}: {
  clients: ClientInfo[];
  currentId?: string;
}) {
  const { t } = useTranslation("overview");
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base flex items-center gap-2">
          <Users className="h-4 w-4" /> {t("connectedClients.title")}
          {clients.length > 0 && (
            <Badge variant="secondary" className="ml-1">
              {clients.length}
            </Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {clients.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("connectedClients.noClients")}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="pb-2 pr-3 font-medium">{t("connectedClients.columns.ip")}</th>
                  <th className="pb-2 px-3 font-medium">{t("connectedClients.columns.user")}</th>
                  <th className="pb-2 px-3 font-medium">{t("connectedClients.columns.role")}</th>
                  <th className="pb-2 pl-3 font-medium">{t("connectedClients.columns.connected")}</th>
                </tr>
              </thead>
              <tbody>
                {clients.map((c) => {
                  const isYou = c.id === currentId;
                  return (
                    <tr
                      key={c.id}
                      className={`border-b last:border-0 ${isYou ? "bg-muted/50" : ""}`}
                    >
                      <td className="py-2 pr-3 font-mono text-xs">
                        {c.remoteAddr}
                        {isYou && (
                          <Badge
                            variant="info"
                            className="ml-1.5 text-[10px] px-1 py-0"
                          >
                            {t("connectedClients.you")}
                          </Badge>
                        )}
                      </td>
                      <td className="py-2 px-3 font-mono text-xs">
                        {c.userId || "--"}
                      </td>
                      <td className="py-2 px-3">
                        <Badge
                          variant={
                            c.role === "admin" || c.role === "owner"
                              ? "default"
                              : c.role === "operator"
                                ? "secondary"
                                : "outline"
                          }
                          className="text-[10px]"
                        >
                          {c.role}
                        </Badge>
                      </td>
                      <td className="py-2 pl-3 text-xs text-muted-foreground whitespace-nowrap">
                        {formatClientTime(c.connectedAt)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
