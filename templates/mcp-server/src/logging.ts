/**
 * Logging utility for MCP servers.
 *
 * CRITICAL: Never use console.log() in MCP stdio servers — it writes to stdout
 * and corrupts the JSON-RPC protocol. Always use console.error() (stderr).
 */

type LogLevel = "debug" | "info" | "warn" | "error";

const LOG_LEVELS: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

const currentLevel = (process.env.LOG_LEVEL as LogLevel) ?? "info";

export function log(level: LogLevel, message: string, ...args: unknown[]) {
  if (LOG_LEVELS[level] < LOG_LEVELS[currentLevel]) return;

  const timestamp = new Date().toISOString();
  const prefix = `[${timestamp}] [${level.toUpperCase()}]`;
  console.error(prefix, message, ...args);
}
