import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";

interface RateValues {
  max_message_chars?: number;
  rate_limit_rpm?: number;
  inbound_debounce_ms?: number;
}

interface Props {
  value: RateValues;
  onChange: (v: RateValues) => void;
}

/** Input rate limiting and message size constraints. */
export function BehaviorRateCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");

  const update = (patch: Partial<RateValues>) => onChange({ ...value, ...patch });

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("behavior.rateLimitTitle")}</CardTitle>
        <CardDescription>{t("behavior.rateLimitDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.maxMessageCharsTip")}>{t("gateway.maxMessageChars")}</InfoLabel>
            <Input
              type="number"
              value={value.max_message_chars ?? ""}
              onChange={(e) => update({ max_message_chars: Number(e.target.value) })}
              placeholder="32000"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.rateLimitRpmTip")}>{t("gateway.rateLimitRpm")}</InfoLabel>
            <Input
              type="number"
              value={value.rate_limit_rpm ?? ""}
              onChange={(e) => update({ rate_limit_rpm: Number(e.target.value) })}
              placeholder="20 (0 = disabled)"
              min={0}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.inboundDebounceMsTip")}>{t("gateway.inboundDebounceMs")}</InfoLabel>
            <Input
              type="number"
              value={value.inbound_debounce_ms ?? ""}
              onChange={(e) => update({ inbound_debounce_ms: Number(e.target.value) })}
              placeholder="0 (no debounce)"
              min={0}
            />
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
