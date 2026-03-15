# Team 02: Software Development 💻

> A small dev squad that develops software features end-to-end — from architecture to implementation to QA.

## GOAL

Develop software features end-to-end with production quality: the Tech Lead architects and reviews, Backend and Frontend developers implement in parallel, and a QA Engineer validates everything.

## Team Composition

| Role | Agent Key | Emoji | Responsibility |
|------|-----------|-------|---------------|
| **Lead** | `tech-lead` | 🏗️ | Tech Lead — architecture, code review, integration |
| Member | `backend-dev` | ⚙️ | Backend Developer — APIs, database, business logic |
| Member | `frontend-dev` | 🎨 | Frontend Developer — UI components, pages, UX |
| Member | `qa-engineer` | 🧪 | QA Engineer — test plans, test code, bug reports |

## Orchestration Pattern

**Mixed (parallel + review):**

```
User requirement → tech-lead
  ├── 1. Analyze & design architecture
  ├── 2. [backend-dev + frontend-dev] in parallel
  │   ├── backend-dev: implements API + DB
  │   └── frontend-dev: implements UI + integration
  ├── 3. qa-engineer reviews & tests (blocked_by: backend + frontend)
  └── 4. tech-lead final review → deliver to user
```

## Example Prompts

```
Build a REST API for a todo list app with CRUD operations.
Use Go for backend, React for frontend. Include authentication.
```

```
Add a user profile feature to the existing app:
- Backend: profile CRUD API + avatar upload
- Frontend: profile page with edit form
- Tests: unit + integration tests
```
