import { generateId } from "@/lib/utils";
import type { ErrorShape, EventFrame, ResponseFrame } from "./protocol";
import { PROTOCOL_VERSION } from "./protocol";
import { ApiError } from "./errors";

type EventListener = (payload: unknown) => void;

interface PendingRequest {
  resolve: (payload: unknown) => void;
  reject: (error: ApiError) => void;
  timeout: ReturnType<typeof setTimeout>;
}

export type ConnectionState = "disconnected" | "connecting" | "connected";

export class WsClient {
  private ws: WebSocket | null = null;
  private pending = new Map<string, PendingRequest>();
  private eventListeners = new Map<string, Set<EventListener>>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempts = 0;
  private authenticated = false;
  private intentionalClose = false;
  private connectGeneration = 0;

  /** Server-assigned role from connect response. */
  role: "admin" | "operator" | "viewer" | "" = "";

  /** Tenant fields from connect response. */
  tenantId = "";
  tenantName = "";
  tenantSlug = "";
  crossTenant = false;
  serverVersion = "";

  private readonly maxReconnectDelay = 30_000;
  private readonly baseReconnectDelay = 1_000;
  private readonly defaultTimeout = 30_000;

  onAuthFailure: (() => void) | null = null;

  onPairingRequired: ((code: string, senderID: string) => void) | null = null;

  constructor(
    private url: string,
    private getToken: () => string,
    private getUserId: () => string,
    private getSenderID: () => string,
    private onStateChange: (state: ConnectionState) => void,
  ) {}

  connect(): void {
    if (this.ws) return;

    this.intentionalClose = false;
    this.onStateChange("connecting");

    const wsUrl = this.buildWsUrl();
    const socket = new WebSocket(wsUrl);
    const generation = ++this.connectGeneration;
    this.ws = socket;

    socket.onopen = () => {
      if (this.ws !== socket) return;
      this.reconnectAttempts = 0;
      this.authenticate(generation);
    };

    socket.onmessage = (event) => {
      this.handleMessage(event.data as string);
    };

    socket.onclose = () => {
      if (this.ws !== socket) return;

      this.ws = null;
      this.authenticated = false;
      this.onStateChange("disconnected");
      this.rejectAllPending("Connection closed");

      if (!this.intentionalClose) {
        this.scheduleReconnect();
      }
    };

    socket.onerror = () => {
      // onclose will fire after onerror
    };
  }

  disconnect(): void {
    this.intentionalClose = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      const socket = this.ws;
      this.ws = null;
      socket.close();
    }
    this.authenticated = false;
    this.rejectAllPending("Disconnected");
    this.onStateChange("disconnected");
  }

  get isConnected(): boolean {
    return this.authenticated && this.ws?.readyState === WebSocket.OPEN;
  }

  /**
   * Reset the timeout for a pending RPC call (e.g. when stream events arrive).
   */
  resetTimeout(requestId: string, timeoutMs: number): void {
    const pending = this.pending.get(requestId);
    if (!pending) return;
    clearTimeout(pending.timeout);
    pending.timeout = setTimeout(() => {
      this.pending.delete(requestId);
      pending.reject(new ApiError("AGENT_TIMEOUT", `timed out after ${timeoutMs}ms of inactivity`));
    }, timeoutMs);
  }

  /**
   * Send an RPC call and wait for the response.
   * Returns { promise, requestId } so callers can reset the timeout on activity.
   */
  callWithId<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
    timeoutMs?: number,
  ): { promise: Promise<T>; requestId: string } {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new ApiError("UNAVAILABLE", "WebSocket not connected");
    }

    const id = generateId();
    const timeout = timeoutMs ?? this.defaultTimeout;

    const promise = new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new ApiError("AGENT_TIMEOUT", `${method} timed out after ${timeout}ms`));
      }, timeout);

      this.pending.set(id, {
        resolve: resolve as (p: unknown) => void,
        reject,
        timeout: timer,
      });

      this.ws!.send(
        JSON.stringify({ type: "req", id, method, params }),
      );
    });

    return { promise, requestId: id };
  }

  /**
   * Send an RPC call and wait for the response.
   */
  async call<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
    timeoutMs?: number,
  ): Promise<T> {
    return this.callWithId<T>(method, params, timeoutMs).promise;
  }

  /**
   * Subscribe to a WebSocket event. Returns an unsubscribe function.
   */
  on(event: string, listener: EventListener): () => void {
    let listeners = this.eventListeners.get(event);
    if (!listeners) {
      listeners = new Set();
      this.eventListeners.set(event, listeners);
    }
    listeners.add(listener);

    return () => {
      listeners!.delete(listener);
      if (listeners!.size === 0) {
        this.eventListeners.delete(event);
      }
    };
  }

  private buildWsUrl(): string {
    if (this.url.startsWith("ws://") || this.url.startsWith("wss://")) {
      return this.url;
    }
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const host = window.location.host;
    return `${proto}//${host}${this.url}`;
  }

  private async authenticate(generation: number): Promise<void> {
    try {
      const res = await this.call<{
        role?: string;
        status?: string;
        pairing_code?: string;
        sender_id?: string;
        tenant_id?: string;
        tenant_name?: string;
        tenant_slug?: string;
        cross_tenant?: boolean;
        server?: { name?: string; version?: string };
      }>("connect", {
        token: this.getToken(),
        user_id: this.getUserId(),
        sender_id: this.getSenderID(),
        locale: localStorage.getItem("goclaw:language") || "en",
        tenant_hint: localStorage.getItem("goclaw:tenant_hint") || "",
        tenant_id: localStorage.getItem("goclaw:tenant_id") || "",
        protocolVersion: PROTOCOL_VERSION,
      });
      if (this.connectGeneration !== generation) return;

      // Browser pairing: server requires approval
      if (res?.status === "pending_pairing" && res.pairing_code && res.sender_id) {
        this.onPairingRequired?.(res.pairing_code, res.sender_id);
        // Keep connection alive for polling browser.pairing.status
        return;
      }

      // Server accepted connection but assigned viewer role → token is invalid
      if (this.getToken() && res?.role === "viewer") {
        this.intentionalClose = true;
        this.ws?.close();
        this.onAuthFailure?.();
        return;
      }

      this.authenticated = true;
      this.role = (res?.role as "admin" | "operator" | "viewer") ?? "";
      this.tenantId = res?.tenant_id ?? "";
      this.tenantName = res?.tenant_name ?? "";
      this.tenantSlug = res?.tenant_slug ?? "";
      this.crossTenant = res?.cross_tenant ?? false;
      this.serverVersion = res?.server?.version ?? "";
      this.onStateChange("connected");
    } catch (e) {
      if (this.connectGeneration === generation) {
        // Tenant access revoked → force logout instead of reconnect
        if (e instanceof ApiError && e.code === "TENANT_ACCESS_REVOKED") {
          this.intentionalClose = true;
          this.ws?.close();
          this.onAuthFailure?.();
          return;
        }
        this.ws?.close();
      }
    }
  }

  private handleMessage(data: string): void {
    let frame: { type: string };
    try {
      frame = JSON.parse(data);
    } catch {
      return;
    }

    if (frame.type === "res") {
      this.handleResponse(frame as ResponseFrame);
    } else if (frame.type === "event") {
      this.handleEvent(frame as EventFrame);
    }
  }

  private handleResponse(frame: ResponseFrame): void {
    const pending = this.pending.get(frame.id);
    if (!pending) return;

    this.pending.delete(frame.id);
    clearTimeout(pending.timeout);

    if (frame.ok) {
      pending.resolve(frame.payload);
    } else {
      const err = frame.error as ErrorShape;
      if (err.code === "UNAUTHORIZED" || err.code === "TENANT_ACCESS_REVOKED") {
        this.onAuthFailure?.();
      }
      pending.reject(
        new ApiError(err.code, err.message, err.details, err.retryable),
      );
    }
  }

  private handleEvent(frame: EventFrame): void {
    const listeners = this.eventListeners.get(frame.event);
    if (listeners) {
      for (const fn of listeners) {
        try {
          fn(frame.payload);
        } catch {
          // Don't let one listener crash others
        }
      }
    }

    const wildcardListeners = this.eventListeners.get("*");
    if (wildcardListeners) {
      for (const fn of wildcardListeners) {
        try {
          fn({ event: frame.event, payload: frame.payload });
        } catch {
          // ignore
        }
      }
    }
  }

  private rejectAllPending(reason: string): void {
    for (const [, req] of this.pending) {
      clearTimeout(req.timeout);
      req.reject(new ApiError("UNAVAILABLE", reason));
    }
    this.pending.clear();
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return;

    const delay = Math.min(
      this.baseReconnectDelay * Math.pow(2, this.reconnectAttempts),
      this.maxReconnectDelay,
    );
    this.reconnectAttempts++;

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }
}
