# GoClaw Selected Cookie Sync Extension

Chrome MV3 extension for explicit selected-cookie sync into GoClaw browser sessions.

Security model:
- No automatic background sync.
- Requests host permission only for the active tab origin.
- Requests gateway-origin permission before sending the selected cookies.
- Sends only checked cookies.
- Sends `userId` and `agentId`; the gateway derives tenant scope from auth.
- Stores extension settings in `chrome.storage.local`.

Load locally from `chrome://extensions` with Developer Mode and "Load unpacked".
