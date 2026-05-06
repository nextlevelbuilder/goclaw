// Package max implements the Max Messenger channel for GoClaw.
//
// Max Messenger (https://max.ru) is a Russian instant messenger. This package
// provides bot integration via the platform's HTTPS Bot API at
// https://platform-api.max.ru.
//
// API documentation: https://dev.max.ru/docs-api
//
// The channel supports:
//   - Direct messages and group chats (bot must be group admin to receive group events)
//   - Long polling (development) and webhooks (production)
//   - Inline keyboards with up to 210 callback buttons (30 rows × 7 buttons)
//   - Markdown and HTML message formatting
//   - Media attachments (images, videos, audio, files, stickers)
//   - Streaming preview via PUT /messages (edit-in-place)
//   - request_contact buttons with HMAC-SHA256 verification
//   - typing indicator via POST /chats/{id}/actions
//
// Bot tokens are obtained from https://business.max.ru/self → Чат-боты → Интеграция.
// Authorization header format: "Authorization: <token>" (no Bearer prefix).
//
// Rate limit: 30 rps per platform-api.max.ru endpoint.
package max
