const FALLBACK_BACKEND = "https://nta-goclaw.gearvn.com.vn";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const backend = env.BACKEND_URL || FALLBACK_BACKEND;

    // Proxy API and WebSocket paths to backend
    if (
      url.pathname.startsWith("/v1/") ||
      url.pathname === "/ws" ||
      url.pathname === "/health" ||
      url.pathname.startsWith("/mcp/")
    ) {
      const target = `${backend}${url.pathname}${url.search}`;
      const headers = new Headers(request.headers);
      headers.set("Host", new URL(backend).host);

      // WebSocket upgrade
      if (request.headers.get("Upgrade") === "websocket") {
        return fetch(target, { headers, body: request.body });
      }

      return fetch(target, {
        method: request.method,
        headers,
        body: request.body,
      });
    }

    // Serve static assets from Pages
    return env.ASSETS.fetch(request);
  },
};
