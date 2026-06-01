import { useEffect, useState } from "react";
import { useHttp } from "@/hooks/use-ws";
import {
  DEFAULT_SKILL_UPLOAD_SIZE_MB,
  normalizeSkillUploadSizeMB,
} from "../lib/validate-skill-zip";

const SKILL_UPLOAD_LIMIT_KEY = "skills.max_upload_size_mb";

export function useSkillUploadLimit(open: boolean): number {
  const http = useHttp();
  const [limit, setLimit] = useState(DEFAULT_SKILL_UPLOAD_SIZE_MB);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;

    http.get<Record<string, string>>("/v1/system-configs")
      .then((configs) => {
        if (!cancelled) {
          setLimit(normalizeSkillUploadSizeMB(Number(configs[SKILL_UPLOAD_LIMIT_KEY])));
        }
      })
      .catch(() => {
        if (!cancelled) setLimit(DEFAULT_SKILL_UPLOAD_SIZE_MB);
      });

    return () => {
      cancelled = true;
    };
  }, [open, http]);

  return limit;
}
