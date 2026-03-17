import { ApiError } from "./errors";

export class HttpClient {
  onAuthFailure: (() => void) | null = null;

  constructor(
    private baseUrl: string,
    private getToken: () => string,
    private getUserId: () => string,
    private getSenderID: () => string = () => "",
  ) {}

  async get<T>(path: string, params?: Record<string, string>): Promise<T> {
    const url = this.buildUrl(path, params);
    return this.request<T>(url, { method: "GET" });
  }

  async post<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(this.buildUrl(path), {
      method: "POST",
      body: body ? JSON.stringify(body) : undefined,
    });
  }

  async put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(this.buildUrl(path), {
      method: "PUT",
      body: body ? JSON.stringify(body) : undefined,
    });
  }

  async delete<T>(path: string): Promise<T> {
    return this.request<T>(this.buildUrl(path), { method: "DELETE" });
  }

  async downloadBlob(path: string): Promise<Blob> {
    const res = await fetch(this.buildUrl(path), {
      method: "GET",
      headers: this.headers(),
    });
    if (!res.ok) {
      throw new ApiError("HTTP_ERROR", res.statusText);
    }
    return res.blob();
  }

  /** Fetch a streaming response (SSE). Returns the raw Response for manual reading. */
  async streamFetch(path: string, signal?: AbortSignal): Promise<Response> {
    const res = await fetch(this.buildUrl(path), {
      method: "GET",
      headers: this.authHeaders(),
      signal,
    });
    if (!res.ok) throw new ApiError("HTTP_ERROR", res.statusText);
    return res;
  }

  /** Build a full URL with auth token as query param (for <img> src, download links). */
  rawUrl(path: string, params?: Record<string, string>): string {
    return this.buildUrl(path, params);
  }

  /** Fetch raw blob with auth headers (for images, binary files). */
  async fetchBlob(path: string, params?: Record<string, string>): Promise<Blob> {
    const url = this.buildUrl(path, params);
    const res = await fetch(url, { method: "GET", headers: this.authHeaders() });
    if (!res.ok) throw new ApiError("HTTP_ERROR", res.statusText);
    return res.blob();
  }

  async upload<T>(path: string, formData: FormData): Promise<T> {
    const headers: Record<string, string> = {};
    const token = this.getToken();
    if (token) headers["Authorization"] = `Bearer ${token}`;
    const userId = this.getUserId();
    if (userId) headers["X-GoClaw-User-Id"] = userId;

    const res = await fetch(this.buildUrl(path), {
      method: "POST",
      headers,
      body: formData,
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new ApiError(
        err.code ?? "HTTP_ERROR",
        err.error ?? err.message ?? res.statusText,
      );
    }

    return res.json() as Promise<T>;
  }

  private buildUrl(path: string, params?: Record<string, string>): string {
    const url = new URL(path, this.baseUrl || window.location.origin);
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        if (v) url.searchParams.set(k, v);
      }
    }
    return url.toString();
  }

  /** Auth-only headers (no Content-Type), for SSE / blob requests. */
  private authHeaders(): Record<string, string> {
    const h: Record<string, string> = {};
    const token = this.getToken();
    if (token) h["Authorization"] = `Bearer ${token}`;
    const userId = this.getUserId();
    if (userId) h["X-GoClaw-User-Id"] = userId;
    const senderID = this.getSenderID();
    if (senderID) h["X-GoClaw-Sender-Id"] = senderID;
    return h;
  }

  private headers(): Record<string, string> {
    return { "Content-Type": "application/json", ...this.authHeaders() };
  }

  private async request<T>(url: string, init: RequestInit): Promise<T> {
    let res: Response;
    try {
      res = await fetch(url, {
        ...init,
        headers: { ...this.headers(), ...(init.headers as Record<string, string>) },
      });
    } catch {
      throw new ApiError("NETWORK_ERROR", "Cannot connect to server. Check if the gateway is running.");
    }

    if (!res.ok) {
      if (res.status === 401) {
        this.onAuthFailure?.();
      }
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new ApiError(
        err.code ?? "HTTP_ERROR",
        err.error ?? err.message ?? res.statusText,
      );
    }

    return res.json() as Promise<T>;
  }
}
