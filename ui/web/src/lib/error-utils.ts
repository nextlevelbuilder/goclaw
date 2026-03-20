import { ApiError, ErrorCodes } from "@/api/errors";
import i18n from "@/i18n";

/** Map API errors to user-friendly messages. Hides 5xx/INTERNAL details from users. */
export function userFriendlyError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === ErrorCodes.INTERNAL || err.code === "HTTP_ERROR" || err.code === ErrorCodes.UNAVAILABLE) {
      return i18n.t("common:errors.serverError");
    }
    return err.message;
  }
  if (err instanceof Error) return err.message;
  return i18n.t("common:errors.serverError");
}
