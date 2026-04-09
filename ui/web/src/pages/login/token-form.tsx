import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { tokenFormSchema, type TokenFormData } from "@/schemas/login.schema";

interface TokenFormProps {
  onSubmit: (userId: string, token: string) => void;
}

export function TokenForm({ onSubmit }: TokenFormProps) {
  const { t } = useTranslation("login");
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<TokenFormData>({
    resolver: zodResolver(tokenFormSchema),
    defaultValues: { userId: "system", token: "" },
  });

  const onValid = async (data: TokenFormData) => {
    setConnecting(true);
    setError(null);

    try {
      const res = await fetch("/v1/agents", {
        headers: {
          Authorization: `Bearer ${data.token.trim()}`,
          "X-GoClaw-User-Id": data.userId.trim(),
        },
      });

      if (res.status === 401) {
        setError(t("token.errorInvalidCredentials"));
        return;
      }

      if (!res.ok) {
        setError(t("token.errorServer", { status: res.status }));
        return;
      }

      onSubmit(data.userId.trim(), data.token.trim());
    } catch {
      setError(t("token.errorCannotConnect"));
    } finally {
      setConnecting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onValid)} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="userId" className="text-sm font-medium">
          {t("token.userId")}
        </Label>
        <Input
          id="userId"
          type="text"
          {...register("userId")}
          placeholder={t("token.userIdPlaceholder")}
          className="text-base md:text-sm"
          autoFocus
          disabled={connecting}
        />
        {errors.userId && (
          <p className="text-xs text-destructive">{errors.userId.message}</p>
        )}
        <p className="text-xs text-muted-foreground">
          {t("token.userIdHint")}
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="token" className="text-sm font-medium">
          {t("token.gatewayToken")}
        </Label>
        <Input
          id="token"
          type="password"
          {...register("token")}
          placeholder={t("token.tokenPlaceholder")}
          className="text-base md:text-sm"
          disabled={connecting}
        />
        {errors.token && (
          <p className="text-xs text-destructive">{errors.token.message}</p>
        )}
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <button
        type="submit"
        disabled={connecting}
        className="inline-flex h-9 w-full items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
      >
        {connecting ? t("token.connecting") : t("token.connect")}
      </button>
    </form>
  );
}
