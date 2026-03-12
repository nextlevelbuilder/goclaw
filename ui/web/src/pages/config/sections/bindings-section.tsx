import { useState, useEffect } from "react";
import { Save, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface BindingMatch {
  channel?: string;
  accountId?: string;
  peer?: { kind?: string; id?: string };
  guildId?: string;
}

interface Binding {
  agentId?: string;
  match?: BindingMatch;
}

interface Props {
  data: Binding[] | undefined;
  onSave: (value: Binding[]) => Promise<void>;
  saving: boolean;
}

const EMPTY_BINDING: Binding = { agentId: "", match: { channel: "", peer: { kind: "", id: "" } } };

export function BindingsSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<Binding[]>(data ?? []);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? []);
    setDirty(false);
  }, [data]);

  const updateBinding = (idx: number, patch: Partial<Binding>) => {
    setDraft((prev) => prev.map((b, i) => (i === idx ? { ...b, ...patch } : b)));
    setDirty(true);
  };

  const updateMatch = (idx: number, patch: Partial<BindingMatch>) => {
    setDraft((prev) =>
      prev.map((b, i) => (i === idx ? { ...b, match: { ...(b.match ?? {}), ...patch } } : b)),
    );
    setDirty(true);
  };

  const updatePeer = (idx: number, patch: Partial<{ kind: string; id: string }>) => {
    setDraft((prev) =>
      prev.map((b, i) =>
        i === idx
          ? { ...b, match: { ...(b.match ?? {}), peer: { ...(b.match?.peer ?? {}), ...patch } } }
          : b,
      ),
    );
    setDirty(true);
  };

  const addBinding = () => {
    setDraft((prev) => [...prev, { ...EMPTY_BINDING, match: { ...EMPTY_BINDING.match, peer: { kind: "", id: "" } } }]);
    setDirty(true);
  };

  const removeBinding = (idx: number) => {
    setDraft((prev) => prev.filter((_, i) => i !== idx));
    setDirty(true);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">{t("bindings.title")}</CardTitle>
            <CardDescription>{t("bindings.description")}</CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={addBinding} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" /> {t("bindings.add")}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {draft.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("bindings.noBindings")}</p>
        ) : (
          draft.map((binding, idx) => (
            <div key={idx} className="flex items-start gap-3 rounded-md border p-3">
              <div className="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <div className="grid gap-1">
                  <Label className="text-xs">{t("bindings.agentId")}</Label>
                  <Input
                    value={binding.agentId ?? ""}
                    onChange={(e) => updateBinding(idx, { agentId: e.target.value })}
                    placeholder="default"
                  />
                </div>
                <div className="grid gap-1">
                  <Label className="text-xs">{t("bindings.channel")}</Label>
                  <Select value={binding.match?.channel ?? ""} onValueChange={(v) => updateMatch(idx, { channel: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder="Any" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="telegram">Telegram</SelectItem>
                      <SelectItem value="discord">Discord</SelectItem>
                      <SelectItem value="slack">Slack</SelectItem>
                      <SelectItem value="whatsapp">WhatsApp</SelectItem>
                      <SelectItem value="zalo">Zalo</SelectItem>
                      <SelectItem value="feishu">Feishu</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1">
                  <Label className="text-xs">{t("bindings.peerKind")}</Label>
                  <Select value={binding.match?.peer?.kind ?? ""} onValueChange={(v) => updatePeer(idx, { kind: v })}>
                    <SelectTrigger>
                      <SelectValue placeholder="Any" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="direct">Direct</SelectItem>
                      <SelectItem value="group">Group</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1">
                  <Label className="text-xs">{t("bindings.peerId")}</Label>
                  <Input
                    value={binding.match?.peer?.id ?? ""}
                    onChange={(e) => updatePeer(idx, { id: e.target.value })}
                    placeholder="Optional"
                  />
                </div>
              </div>
              <Button variant="ghost" size="icon" className="mt-5 shrink-0" onClick={() => removeBinding(idx)}>
                <Trash2 className="h-4 w-4 text-muted-foreground" />
              </Button>
            </div>
          ))
        )}

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
