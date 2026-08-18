---
name: ui-ux-pro-max
description: Review or build web UI against GoClaw's mobile/UI/UX rule checklist with precise pass criteria (architect /gc:uiux)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - ui_change
  - screen_name
outputs:
  - rule_by_rule_assessment
  - violation_list
allowed-tools:
  - search
  - read_file
  - filesystem
quality-gates:
  - every_rule_assessed
  - violations_located
---

# UI/UX Pro Max

Audit or build a web UI against the GoClaw mobile/UI/UX rule set. Each rule has
a precise pass/fail criterion — never grade a rule "mostly OK". A rule is a
finding only when a concrete, locatable violation exists.

## Purpose

`/gc:uiux` applies the GoClaw mobile/UI/UX checklist to a screen or a change.
The checklist is the product of real incidents: iOS auto-zoom on small inputs,
content hidden under browser chrome, unclickable dropdowns inside Radix
dialogs, and full-page remounts from unstable error-boundary keys. Every rule
exists because a shipped screen broke without it.

## Operating Rules

- State the scope first: the screen, the route, the touched components.
- Ground every finding in `file:line`. No location, no finding.
- Apply the checklist rules in the order below. Mark each as pass or fail.
- A "pass" must name the evidence (the file/line that satisfies the rule).
- Never fix code during `/gc:uiux` review — list findings; the fix belongs to `/gc:cook`.

## The checklist

Work these rules in order. For each, decide pass/fail with evidence.

### 1. Viewport height

- Rule: use `h-dvh` (dynamic viewport height), never `h-screen`.
- Why: `h-screen` is the visual viewport on mobile; browser chrome and virtual
  keyboards overlap it, pushing content off-screen.
- Pass: no `h-screen` in the reviewed scope; full-height containers use `h-dvh`.

### 2. Input font size

- Rule: every `<input>`, `<textarea>`, `<select>` uses `text-base md:text-sm`
  (16px on mobile).
- Why: font-size below 16px triggers iOS Safari auto-zoom on focus.
- Pass: every input element carries a mobile font size of at least 16px
  (`text-base` or an explicit ≥16px value).

### 3. Safe areas

- Rule: the root layout sets `viewport-fit=cover` in the meta viewport tag;
  edge-anchored elements (app shell, sidebar, toasts, chat input) use
  `safe-top`, `safe-bottom`, `safe-left`, `safe-right` utilities.
- Why: notched and rounded-corner devices overlap edge content without insets.
- Pass: viewport meta includes `viewport-fit=cover` and edge-anchored elements
  reference the safe-area utilities.

### 4. Touch targets

- Rule: icon buttons have a ≥44px hit area on touch devices. CSS uses
  `@media (pointer: coarse)` with `::after` pseudo-elements to expand targets.
- Why: sub-44px targets fail Apple/Google accessibility guidance and get
  mis-tapped.
- Pass: every icon-only control either measures ≥44px in both axes or has a
  coarse-pointer expansion rule in CSS.

### 5. Tables

- Rule: every `<table>` is wrapped in `<div className="overflow-x-auto">` and
  the table sets `min-w-[600px]`.
- Why: tables without horizontal scroll crush columns on narrow screens.
- Pass: no bare `<table>`; each has the overflow wrapper and min-width.

### 6. Responsive grids

- Rule: grids use mobile-first breakpoints — `grid-cols-1 sm:grid-cols-2 lg:grid-cols-N`.
- Why: fixed `grid-cols-N` without a mobile breakpoint renders all columns on a
  phone.
- Pass: no `grid-cols-N` (N>1) that lacks a `sm:` or `lg:` prefix.

### 7. Dialogs

- Rule: dialogs are full-screen on mobile with slide-up animation
  (`max-sm:inset-0`), centered with zoom on desktop (`sm:max-w-lg`).
- Why: `max-w-lg` alone is unreadable on phones; the slide-up matches native
  sheet conventions.
- Pass: the dialog container applies `max-sm:inset-0` and `sm:max-w-lg`.

### 8. Virtual keyboard

- Rule: the chat input uses the `useVirtualKeyboard()` hook and
  `var(--keyboard-height, 0px)` to stay above the soft keyboard.
- Why: fixed-height layouts hide the composer when the keyboard opens.
- Pass: chat input bottom offset derives from `--keyboard-height`.

### 9. Scroll behavior

- Rule: scrollable areas use `overscroll-contain` to prevent background scroll
  chaining; auto-scroll is smooth for incoming messages and instant on user send.
- Why: scroll chaining drags the page with the chat; jarring auto-scroll
  disorients the reader.
- Pass: scroll containers set `overscroll-contain`; auto-scroll uses smooth for
  inbound, instant for outbound.

### 10. Landscape

- Rule: top bars add the `landscape-compact` class to reduce padding when the
  phone is in landscape (`max-height: 500px`).
- Why: portrait padding consumes the shallow landscape viewport.
- Pass: top bars include `landscape-compact` with a `max-height: 500px` media
  rule in CSS.

### 11. Portaled dropdowns in dialogs

- Rule: custom dropdown components that use
  `createPortal(content, document.body)` MUST add `pointer-events-auto` to the
  dropdown element. Radix-native portals (Select, Popover) handle this.
- Why: Radix Dialog sets `pointer-events: none` on `document.body`; without the
  override, portaled dropdowns are unclickable.
- Pass: every custom-portal dropdown in a dialog carries `pointer-events-auto`.

### 12. Timezone formatting

- Rule: use `Intl.DateTimeFormat` via `formatBucketTz()` from `lib/format.ts` —
  no date-fns-tz dependency. User timezone comes from `useUiStore`.
- Why: a TZ database dependency is unnecessary and diverges from the app's
  native formatting path.
- Pass: no new date-fns-tz import in the reviewed scope; timezone-aware
  formatting routes through `formatBucketTz`.

### 13. Error boundary stability

- Rule: `AppLayout` wraps `<Outlet>` in
  `<ErrorBoundary key={stableErrorBoundaryKey(pathname)}>` which strips dynamic
  segments (`/chat/session-A` → `/chat`). Never use `key={location.pathname}`.
- Why: an unstable key remounts the whole page on param changes, causing state
  loss and flash on every navigation.
- Pass: error-boundary keys derive from the stable (segment-stripped) path;
  detail pages share a stable key.

### 14. Route params as source of truth

- Rule: pages with URL params (e.g. `/chat/:sessionKey`) derive state from
  `useParams()`, never duplicate into `useState`; use optional params
  (`/chat/:sessionKey?`) rather than two routes.
- Why: duplicated state races `navigate()`, producing UI bounce (B→A→B).
- Pass: no `useState` mirror of a route param; single route with optional param.

## Workflow

1. **Scope** — name the screen, route, and touched component files.
2. **Read** — read the components, their CSS, and the layout shell.
3. **Assess** — walk the checklist, one rule at a time, recording pass/fail
   with evidence.
4. **Report** — list violations sorted by severity (BLOCKER/CRITICAL/HIGH/
   MEDIUM/LOW/INFO), each with `file:line`, the violated rule, and the precise
   criterion it fails.

## Quality gates

Confirm both before finishing:

- **every_rule_assessed** — all 14 checklist rules are marked pass or fail with
  evidence, none skipped.
- **violations_located** — every violation has `file:line` and the rule it
  breaks.

Do not claim the audit complete until both gates pass.
