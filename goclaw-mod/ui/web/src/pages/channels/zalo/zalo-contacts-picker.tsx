import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useWsCall } from "@/hooks/use-ws-call";

interface Friend {
  userId: string;
  displayName: string;
  zaloName?: string;
  avatar?: string;
}

interface Group {
  groupId: string;
  name: string;
  avatar?: string;
  totalMember: number;
}

interface ContactsResult {
  friends: Friend[];
  groups: Group[];
}

interface ZaloContactsPickerProps {
  instanceId: string;
  hasCredentials: boolean;
  value: string[];
  onChange: (ids: string[]) => void;
}

export function ZaloContactsPicker({ instanceId, hasCredentials, value, onChange }: ZaloContactsPickerProps) {
  const { t } = useTranslation("channels");
  const [contacts, setContacts] = useState<ContactsResult | null>(null);
  const [search, setSearch] = useState("");
  const [manualId, setManualId] = useState("");
  const { loading, error, call: fetchContacts } = useWsCall<ContactsResult>("zalo.personal.contacts");
  const autoLoaded = useRef(false);

  // Auto-load contacts when credentials are available (resolve names for badges)
  useEffect(() => {
    if (hasCredentials && !autoLoaded.current) {
      autoLoaded.current = true;
      fetchContacts({ instance_id: instanceId }).then(setContacts)
        .catch((err) => console.error("[ZaloContactsPicker] auto-load failed:", err));
    }
  }, [hasCredentials, instanceId, fetchContacts]);

  const handleLoad = async () => {
    try {
      const result = await fetchContacts({ instance_id: instanceId });
      setContacts(result);
    } catch {
      // error state handled by useWsCall
    }
  };

  const toggle = (id: string) => {
    if (value.includes(id)) {
      onChange(value.filter((v) => v !== id));
    } else {
      onChange([...value, id]);
    }
  };

  const addManual = () => {
    const trimmed = manualId.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
      setManualId("");
    }
  };

  const resolveName = (id: string): string => {
    const friend = contacts?.friends.find((f) => f.userId === id);
    if (friend) return friend.displayName;
    const group = contacts?.groups.find((g) => g.groupId === id);
    if (group) return group.name;
    return id;
  };

  if (!hasCredentials) {
    return (
      <div className="grid gap-1.5">
        <Label>{t("zalo.allowedUsers")}</Label>
        <p className="text-sm text-muted-foreground">{t("zalo.completeQrLogin")}</p>
      </div>
    );
  }

  const lowerSearch = search.toLowerCase();
  const filteredFriends = contacts?.friends.filter(
    (f) => f.displayName.toLowerCase().includes(lowerSearch) || f.userId.includes(search),
  ) ?? [];
  const filteredGroups = contacts?.groups.filter(
    (g) => g.name.toLowerCase().includes(lowerSearch) || g.groupId.includes(search),
  ) ?? [];

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Label>{t("zalo.allowedUsers")}</Label>
        {!contacts && (
          <Button type="button" variant="outline" size="sm" onClick={handleLoad} disabled={loading}>
            {loading ? t("zalo.loading") : t("zalo.loadContacts")}
          </Button>
        )}
      </div>

      {error && <p className="text-sm text-destructive">{error.message}</p>}

      {/* Selected tags */}
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {value.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1">
              {resolveName(id)}
              <button type="button" onClick={() => toggle(id)} className="ml-1 text-xs hover:text-destructive">
                ×
              </button>
            </Badge>
          ))}
        </div>
      )}

      {/* Contact list (after loading) */}
      {contacts && (
        <>
          <Input
            placeholder={t("zalo.searchContacts")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8"
          />
          <Tabs defaultValue="friends">
            <TabsList className="w-full">
              <TabsTrigger value="friends" className="flex-1 text-xs">
                {t("zalo.friends")} ({filteredFriends.length})
              </TabsTrigger>
              <TabsTrigger value="groups" className="flex-1 text-xs">
                {t("zalo.groups")} ({filteredGroups.length})
              </TabsTrigger>
            </TabsList>
            <TabsContent value="friends" className="mt-2">
              <div className="max-h-56 overflow-y-auto rounded border p-2 space-y-1">
                {filteredFriends.length > 0 ? filteredFriends.map((f) => (
                  <label key={f.userId} className="flex items-center gap-2 py-0.5 text-sm cursor-pointer hover:bg-muted/50 rounded px-1">
                    <input type="checkbox" checked={value.includes(f.userId)} onChange={() => toggle(f.userId)} />
                    <span className="truncate">{f.displayName}</span>
                    <span className="text-xs text-muted-foreground ml-auto shrink-0">{f.userId}</span>
                  </label>
                )) : (
                  <p className="text-sm text-muted-foreground py-2 text-center">
                    {search ? t("zalo.noContactsMatch", { search }) : t("zalo.noContacts")}
                  </p>
                )}
              </div>
            </TabsContent>
            <TabsContent value="groups" className="mt-2">
              <div className="max-h-56 overflow-y-auto rounded border p-2 space-y-1">
                {filteredGroups.length > 0 ? filteredGroups.map((g) => (
                  <label key={g.groupId} className="flex items-center gap-2 py-0.5 text-sm cursor-pointer hover:bg-muted/50 rounded px-1">
                    <input type="checkbox" checked={value.includes(g.groupId)} onChange={() => toggle(g.groupId)} />
                    <span className="truncate">{g.name}</span>
                    <span className="text-xs text-muted-foreground ml-auto shrink-0">{t("zalo.membersCount", { count: g.totalMember })}</span>
                  </label>
                )) : (
                  <p className="text-sm text-muted-foreground py-2 text-center">
                    {search ? t("zalo.noContactsMatch", { search }) : t("zalo.noContacts")}
                  </p>
                )}
              </div>
            </TabsContent>
          </Tabs>
        </>
      )}

      {/* Manual ID entry */}
      <div className="flex gap-2">
        <Input
          placeholder={t("zalo.addManualPlaceholder")}
          value={manualId}
          onChange={(e) => setManualId(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addManual(); } }}
          className="h-8"
        />
        <Button type="button" variant="outline" size="sm" onClick={addManual} disabled={!manualId.trim()}>
          {t("zalo.add")}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">{t("zalo.zaloIdsHint")}</p>
    </div>
  );
}
