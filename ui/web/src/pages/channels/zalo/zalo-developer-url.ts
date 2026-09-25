const zaloDeveloperAppBase = "https://developers.zalo.me/app";

export function zaloDeveloperAppURL(value: unknown): string {
  if (typeof value !== "string" && typeof value !== "number") return "";
  const appID = String(value).trim();
  if (appID === "" || appID === "***") return "";
  return `${zaloDeveloperAppBase}/${encodeURIComponent(appID)}`;
}

export function zaloWebhookSettingsURL(value: unknown): string {
  const developerAppURL = zaloDeveloperAppURL(value);
  return developerAppURL ? `${developerAppURL}/webhook` : "";
}
