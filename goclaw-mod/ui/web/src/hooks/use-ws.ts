import { createContext, useContext } from "react";
import type { WsClient } from "@/api/ws-client";
import type { HttpClient } from "@/api/http-client";

interface WsContextValue {
  ws: WsClient;
  http: HttpClient;
}

export const WsContext = createContext<WsContextValue | null>(null);

export function useWs(): WsClient {
  const ctx = useContext(WsContext);
  if (!ctx) throw new Error("useWs must be used within <WsProvider>");
  return ctx.ws;
}

export function useHttp(): HttpClient {
  const ctx = useContext(WsContext);
  if (!ctx) throw new Error("useHttp must be used within <WsProvider>");
  return ctx.http;
}
