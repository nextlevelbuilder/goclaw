import type { HookFormData } from "@/schemas/hooks.schema";

function parseHeaders(raw: string | undefined): Record<string, unknown> {
  const trimmed = (raw ?? "").trim();
  if (!trimmed) return {};
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
    throw new Error("headers must be a JSON object");
  } catch (err) {
    // The original parser message is included verbatim in the user-facing error.
    // eslint-disable-next-line preserve-caught-error
    throw new Error(
      "Invalid headers JSON: " + (err instanceof Error ? err.message : String(err)),
    );
  }
}

export function buildHookConfig(data: HookFormData): Record<string, unknown> {
  if (data.handler_type === "http") {
    return {
      url: data.url ?? "",
      method: data.method ?? "POST",
      headers: parseHeaders(data.headers),
      body_template: data.body_template ?? "",
    };
  }
  if (data.handler_type === "script") {
    return { source: data.script_source ?? "" };
  }
  return {
    prompt_template: data.prompt_template ?? "",
    provider: data.provider ?? "",
    model: data.model ?? "",
    max_invocations_per_turn: data.max_invocations_per_turn ?? 5,
  };
}
