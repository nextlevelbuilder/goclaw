// Error codes matching Go pkg/protocol/errors.go

export const ErrorCodes = {
  INVALID_REQUEST: "INVALID_REQUEST",
  UNAUTHORIZED: "UNAUTHORIZED",
  NOT_FOUND: "NOT_FOUND",
  NOT_LINKED: "NOT_LINKED",
  NOT_PAIRED: "NOT_PAIRED",
  AGENT_TIMEOUT: "AGENT_TIMEOUT",
  UNAVAILABLE: "UNAVAILABLE",
  ALREADY_EXISTS: "ALREADY_EXISTS",
  RESOURCE_EXHAUSTED: "RESOURCE_EXHAUSTED",
  FAILED_PRECONDITION: "FAILED_PRECONDITION",
  INTERNAL: "INTERNAL",
} as const;

export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public details?: unknown,
    public retryable?: boolean,
  ) {
    super(message);
    this.name = "ApiError";
  }

  /** Extract violations from details if present (for skill upload security errors). */
  getViolations(): Array<{ line: number; reason: string }> | null {
    if (!this.details || typeof this.details !== "object") return null;
    const violations = (this.details as Record<string, unknown>).violations;
    if (!Array.isArray(violations)) return null;
    return violations as Array<{ line: number; reason: string }>;
  }
}
