---
name: voice-session-summarization
description: Summarize a Discord voice session transcript using semantic memory context (contributor identities, project terminology, recent prior sessions) from the agent's memory vault. Project-agnostic — works for any Obsidian-style vault.
---

# Voice Session Summarization

You are summarizing a Discord voice channel conversation for a transcript channel. The user message contains the transcript with each line in the form `<speaker name>: <what they said>`. Your job is to produce a tight summary suitable for a Discord channel message: 2-4 short paragraphs OR a 3-6 bullet list, whichever fits the conversation better.

## Required steps

You have access to memory tools (`memory_search`, `memory_get`, `memory_backlinks`) over the agent's memory vault. Use them to ground the summary in real context before writing a single sentence.

### 1. Speaker identity resolution

For each unique speaker prefix (`<DisplayName>:`), call `memory_search` with the display name as the query. Contributor / person pages typically surface as the top result with `type: contributor` (or similar) in their frontmatter.

If a clearly canonical name appears in a high-ranking result's frontmatter `aliases` or title, normalize the speaker to that canonical form when you write the summary. For example: a Cartridge vault might map `mataleone` → contributor page `Mateusz Chudkowski`; you'd write `Mateusz` in the summary, not `mataleone`. The same mechanism resolves any team's people — there are no hardcoded names; only what the vault tells you.

### 2. Topic identification

Identify product / project / component nouns in the transcript. For each candidate, `memory_search` semantically. If the top result is a definition page (frontmatter `type` in `project | product | component | concept | theme`), reference it via wikilink `[[Page Title]]` in the summary. This gives the transcript channel real cross-references back to the wiki, which Obsidian renders as backlinks for free.

### 3. Temporal context

`memory_search` for:
- Recent prior voice-session entries — query for the channel name + recent date.
- For any project resolved in step 2, query `<project> weekly|update|recent activity` to surface the latest project rollup.

Consume the top ~5 snippets as background. When the current call references a thread that was discussed before, mention it briefly ("continuing last week's [[Glitch Bomb]] discussion …").

### 4. Compose

- 2-4 short paragraphs OR 3-6 bullets, whichever the conversation shape calls for.
- ≤1500 characters total (Discord-compatible).
- Lead with the topic / decision, not generic preamble.
- Use canonical names + wikilinks per steps 1-2.
- Quote at most one short, distinctive line if it's load-bearing.
- Skip filler ("uh", "you know"), greetings, side-channel chatter.
- If the conversation was very short or content-free, just say so in one sentence.
- Plain text — no Markdown headings; small inline emphasis (italics, bold) is fine.

### 5. Persist (optional — only when configured)

If a session output directory was provided (typically as `VOICE_SESSION_OUTPUT_DIR` env var or the `session_output_dir` field on your invocation), write a new memory entry there using `write_file`:

Path: `<session_output_dir>/<YYYY-MM-DD>/<HHMM>-<channel-slug>.md`

Frontmatter:
```yaml
---
title: <channel> voice session — <human time>
type: voice-session
updated: <ISO date>
participants: [<canonical names from step 1>]
tags: [voice, <project-slugs from step 2>]
sources: [<discord message URL if available>]
---
```

Body: the summary text from step 4.

The disk seeder (`internal/memory/disk_seeder.go`) picks up this new file within its next sweep (default: 5 min) and indexes it. Wikilinks back to projects + contributors automatically yield Obsidian backlinks.

### 6. Return

Return only the summary body text (the step-4 output, not the frontmatter). This becomes the response posted to Discord.

## Project-agnostic design

This skill makes no assumptions about a specific vault layout. It uses semantic `memory_search` to discover contributor pages, project pages, and prior session entries — so the same skill runs unchanged against any team's vault that uses Obsidian frontmatter + wikilink conventions. Project-specific knowledge (the actual contributor names, the actual product taxonomy) lives in the vault content, not in this skill.
