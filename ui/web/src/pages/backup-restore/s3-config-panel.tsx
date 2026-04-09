import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { useS3Config, useSaveS3Config } from "./hooks/use-s3-config";
import { s3ConfigSchema, type S3ConfigFormData } from "@/schemas/s3-config.schema";

export function S3ConfigPanel() {
  const { t } = useTranslation("backup");
  const { data, isLoading } = useS3Config();
  const saveMutation = useSaveS3Config();

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<S3ConfigFormData>({
    resolver: zodResolver(s3ConfigSchema),
    defaultValues: {
      access_key_id: "",
      secret_access_key: "",
      bucket: "",
      region: "us-east-1",
      endpoint: "",
      prefix: "backups/",
    },
  });

  // Populate form on initial load only (not after save-triggered refetch)
  const hydrated = useRef(false);
  useEffect(() => {
    if (data?.configured && !hydrated.current) {
      hydrated.current = true;
      reset({
        access_key_id: data.access_key_id ?? "",
        secret_access_key: "",
        bucket: data.bucket ?? "",
        region: data.region ?? "us-east-1",
        endpoint: data.endpoint ?? "",
        prefix: data.prefix ?? "backups/",
      });
    }
  }, [data, reset]);

  const onValid = (data: S3ConfigFormData) => {
    saveMutation.mutate(data);
  };

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading...
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <h3 className="text-sm font-medium">{t("s3.title")}</h3>
        <Badge variant={data?.configured ? "default" : "secondary"}>
          {data?.configured ? t("s3.status.configured") : t("s3.status.notConfigured")}
        </Badge>
      </div>
      <p className="text-sm text-muted-foreground">{t("s3.description")}</p>

      <form onSubmit={handleSubmit(onValid)} className="space-y-4">
        <div>
          <Label htmlFor="s3-key">{t("s3.fields.accessKeyId")}</Label>
          <Input id="s3-key" {...register("access_key_id")}
            className="mt-1 text-base md:text-sm" />
          {errors.access_key_id && (
            <p className="mt-1 text-xs text-destructive">{errors.access_key_id.message}</p>
          )}
        </div>

        <div>
          <Label htmlFor="s3-secret">{t("s3.fields.secretAccessKey")}</Label>
          <Input id="s3-secret" type="password" {...register("secret_access_key")}
            placeholder={data?.configured ? t("s3.fields.secretPlaceholder") : undefined}
            className="mt-1 text-base md:text-sm" />
        </div>

        <div>
          <Label htmlFor="s3-bucket">{t("s3.fields.bucket")}</Label>
          <Input id="s3-bucket" {...register("bucket")}
            className="mt-1 text-base md:text-sm" />
          {errors.bucket && (
            <p className="mt-1 text-xs text-destructive">{errors.bucket.message}</p>
          )}
        </div>

        <div>
          <Label htmlFor="s3-region">{t("s3.fields.region")}</Label>
          <Input id="s3-region" {...register("region")}
            className="mt-1 text-base md:text-sm" />
        </div>

        <div>
          <Label htmlFor="s3-endpoint">{t("s3.fields.endpoint")}</Label>
          <Input id="s3-endpoint" {...register("endpoint")}
            placeholder="https://..." className="mt-1 text-base md:text-sm" />
          <p className="mt-1 text-xs text-muted-foreground">{t("s3.fields.endpointHint")}</p>
        </div>

        <div>
          <Label htmlFor="s3-prefix">{t("s3.fields.prefix")}</Label>
          <Input id="s3-prefix" {...register("prefix")}
            className="mt-1 text-base md:text-sm" />
        </div>

        <div className="flex justify-end pt-2">
          <Button type="submit" disabled={saveMutation.isPending}>
            {saveMutation.isPending && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
            {saveMutation.isPending ? t("s3.saving") : t("s3.save")}
          </Button>
        </div>
      </form>
    </div>
  );
}
