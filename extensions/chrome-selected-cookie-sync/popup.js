const state = {
  tab: null,
  originPattern: "",
  cookies: [],
};

const els = {
  gatewayUrl: document.getElementById("gatewayUrl"),
  token: document.getElementById("token"),
  userId: document.getElementById("userId"),
  agentId: document.getElementById("agentId"),
  grant: document.getElementById("grant"),
  refresh: document.getElementById("refresh"),
  selectAll: document.getElementById("selectAll"),
  sync: document.getElementById("sync"),
  cookies: document.getElementById("cookies"),
  status: document.getElementById("status"),
};

init().catch((err) => setStatus(err.message, true));

async function init() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  state.tab = tab;
  state.originPattern = originPatternFor(tab?.url || "");
  const settings = await chrome.storage.local.get(["gatewayUrl", "token", "userId", "agentId"]);
  els.gatewayUrl.value = settings.gatewayUrl || "http://localhost:18790";
  els.token.value = settings.token || "";
  els.userId.value = settings.userId || "";
  els.agentId.value = settings.agentId || "default";
  bindEvents();
  await loadCookies();
}

function bindEvents() {
  for (const input of [els.gatewayUrl, els.token, els.userId, els.agentId]) {
    input.addEventListener("change", saveSettings);
  }
  els.grant.addEventListener("click", requestSiteAccess);
  els.refresh.addEventListener("click", loadCookies);
  els.selectAll.addEventListener("click", selectAllCookies);
  els.sync.addEventListener("click", syncSelected);
}

async function saveSettings() {
  await chrome.storage.local.set({
    gatewayUrl: els.gatewayUrl.value.trim(),
    token: els.token.value,
    userId: els.userId.value.trim(),
    agentId: els.agentId.value.trim(),
  });
}

async function requestSiteAccess() {
  if (!state.originPattern) {
    setStatus("Active tab is not an HTTP site.", true);
    return;
  }
  const granted = await chrome.permissions.request({ origins: [state.originPattern] });
  if (!granted) {
    setStatus("Site access not granted.", true);
    return;
  }
  await loadCookies();
}

async function loadCookies() {
  if (!state.tab?.url || !state.originPattern) {
    setStatus("Open an HTTP site tab first.", true);
    return;
  }
  const hasPermission = await chrome.permissions.contains({ origins: [state.originPattern] });
  if (!hasPermission) {
    state.cookies = [];
    renderCookies();
    setStatus("Grant access for this site before reading cookies.");
    return;
  }
  state.cookies = await chrome.cookies.getAll({ url: state.tab.url });
  renderCookies();
  setStatus(`${state.cookies.length} cookies available for this site.`);
}

function renderCookies() {
  els.cookies.textContent = "";
  if (state.cookies.length === 0) {
    const empty = document.createElement("div");
    empty.className = "cookie-meta";
    empty.textContent = "No cookies loaded.";
    els.cookies.append(empty);
    return;
  }
  for (const cookie of state.cookies) {
    const row = document.createElement("label");
    row.className = "cookie-row";
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.dataset.cookieKey = cookieKey(cookie);
    const body = document.createElement("div");
    const name = document.createElement("div");
    name.className = "cookie-name";
    name.textContent = cookie.name;
    const meta = document.createElement("div");
    meta.className = "cookie-meta";
    meta.textContent = `${cookie.domain}${cookie.path || "/"}${cookie.httpOnly ? " · HttpOnly" : ""}${cookie.secure ? " · Secure" : ""}`;
    body.append(name, meta);
    row.append(checkbox, body);
    els.cookies.append(row);
  }
}

function selectAllCookies() {
  for (const checkbox of els.cookies.querySelectorAll("input[type='checkbox']")) {
    checkbox.checked = true;
  }
}

async function syncSelected() {
  await saveSettings();
  const selectedKeys = new Set(
    [...els.cookies.querySelectorAll("input[type='checkbox']:checked")].map((el) => el.dataset.cookieKey),
  );
  const cookies = state.cookies.filter((cookie) => selectedKeys.has(cookieKey(cookie)));
  if (cookies.length === 0) {
    setStatus("Select at least one cookie.", true);
    return;
  }
  const gatewayUrl = els.gatewayUrl.value.trim().replace(/\/+$/, "");
  const userId = els.userId.value.trim();
  const agentId = els.agentId.value.trim();
  if (!gatewayUrl || !userId || !agentId) {
    setStatus("Gateway URL, User ID, and Agent ID are required.", true);
    return;
  }
  if (!(await ensureOriginPermission(gatewayUrl))) {
    setStatus("Gateway access not granted.", true);
    return;
  }
  const headers = {
    "Content-Type": "application/json",
    "X-GoClaw-User-Id": userId,
  };
  if (els.token.value) {
    headers.Authorization = `Bearer ${els.token.value}`;
  }
  const response = await fetch(`${gatewayUrl}/v1/browser/cookies/sync`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      agent_id: agentId,
      source: "chrome-selected-cookie-sync",
      cookies: cookies.map((cookie) => ({
        domain: cookie.domain,
        name: cookie.name,
        path: cookie.path,
        value: cookie.value,
        secure: cookie.secure,
        httpOnly: cookie.httpOnly,
        sameSite: cookie.sameSite,
        expirationDate: cookie.expirationDate,
      })),
    }),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    setStatus(data.error || `Sync failed with HTTP ${response.status}.`, true);
    return;
  }
  setStatus(`Synced ${data.synced ?? cookies.length} cookies.`);
}

async function ensureOriginPermission(rawUrl) {
  const pattern = originPatternFor(rawUrl);
  if (!pattern) return false;
  if (await chrome.permissions.contains({ origins: [pattern] })) return true;
  return chrome.permissions.request({ origins: [pattern] });
}

function originPatternFor(rawUrl) {
  try {
    const url = new URL(rawUrl);
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    return `${url.protocol}//${url.host}/*`;
  } catch {
    return "";
  }
}

function cookieKey(cookie) {
  return `${cookie.domain}\n${cookie.path}\n${cookie.name}`;
}

function setStatus(message, isError = false) {
  els.status.textContent = message;
  els.status.style.color = isError ? "#b42318" : "#526074";
}
