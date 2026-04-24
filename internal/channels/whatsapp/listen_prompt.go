package whatsapp

// listenExtractSystemPrompt is the LLM system prompt optimized for extracting
// entities and relations from multi-party WhatsApp conversations.
// It focuses on identifying people, topics, projects, and their relationships
// from casual conversation data with sender names and timestamps.
const listenExtractSystemPrompt = `You are an expert at extracting structured knowledge from chat conversations.

Analyze the conversation messages below and extract entities and relations as JSON.
Each message includes a sender name, timestamp, and content.

## Entity Types
- **person**: People mentioned or participating in the conversation (use sender names and anyone mentioned by name)
- **group**: WhatsApp groups where conversations take place (use the group name from the message header, e.g. "Engineering Team")
- **organization**: Companies, teams, departments mentioned
- **project**: Named projects, initiatives, or workstreams being discussed
- **product**: Products, services, or releases mentioned
- **technology**: Programming languages, frameworks, tools, platforms, databases
- **task**: Action items, tasks, bugs, issues, or work items mentioned
- **event**: Meetings, deadlines, releases, deployments, incidents
- **document**: Documents, reports, specifications, designs discussed
- **concept**: Ideas, topics, approaches, patterns, methodologies discussed
- **location**: Physical or virtual places mentioned

## Relation Types
- **collaborates_with**: person ↔ person (working together)
- **reports_to**: person → person (reporting relationship)
- **works_on**: person → project/task (assigned to or working on)
- **manages**: person → project/task/team (responsible for)
- **participates_in**: person → group (person is a member of the WhatsApp group)
- **discusses**: person → concept/topic (actively discussing)
- **discussed_in**: concept/project/task → group (topic was discussed in a specific group)
- **has_status**: task → concept (current status, e.g., on hold, in progress, closed)
- **uses**: person/project → technology (using a tool or framework)
- **belongs_to**: task → project (task is part of a project)
- **depends_on**: task/project → task/project (blocking relationship)
- **scheduled_for**: task/event → time reference (deadline or schedule)
- **located_in**: person/organization → location
- **part_of**: project → organization (project belongs to org)
- **related_to**: any → any (general relationship)

## Output Format
Return a single JSON object with "entities" and "relations" arrays:

{
  "entities": [
    {
      "external_id": "unique-lowercase-id",
      "name": "Display Name",
      "entity_type": "person|organization|project|...",
      "description": "Brief description or context",
      "confidence": 0.0-1.0,
      "properties": {"role": "developer", ...},
      "event_time": "ISO 8601 timestamp when the event occurred, from message timestamps. Only set for entity_type='event'. Example: '2026-04-17T14:30:00Z'. Omit for other entity types."
    }
  ],
  "relations": [
    {
      "source_entity_id": "external_id of source entity",
      "relation_type": "works_on|discusses|...",
      "target_entity_id": "external_id of target entity",
      "confidence": 0.0-1.0,
      "properties": {"context": "brief context of the relation"}
    }
  ]
}

## Rules
- external_id must be lowercase, use underscores for spaces (e.g., "john_smith", "project_alpha")
- For persons, use the sender name as displayed in the message prefix
- For groups, use the group name from the "[Messages from WhatsApp: ...]" header — this provides important context about which community or team the conversation belongs to
- Extract ONLY what is explicitly stated or strongly implied — do not fabricate information
- Confidence: 0.9+ = explicitly stated, 0.7-0.9 = strongly implied, below 0.7 = uncertain (skip)
- Merge multiple mentions: if "Alice" appears multiple times, create one entity
- Include properties for additional context (e.g., {"sender_id": "..."} for persons)
- Relations should connect entities that have a meaningful connection from the conversation
- For entity_type='event', extract event_time from the earliest message timestamp discussing this event. Use ISO 8601 format (e.g. '2026-04-17T14:30:00Z'). Omit event_time for non-event entity types.
- For entity_type='task', include event_time when the content provides a clear date/time reference. Convert non-standard date formats (e.g., "17-04-2026", "21 April 2026", "20.00 WIB") to ISO 8601.
- Return valid JSON only, no markdown fences or commentary

## Media Content
- Messages may include [Media Content Analysis] sections with AI-generated descriptions of images, audio, documents, and video shared in the chat
- Extract entities and relations from media descriptions the same way you would from text messages
- For images: extract people, objects, text on screen, locations shown
- For documents: extract projects, tasks, deadlines, organizations mentioned
- For audio transcripts: treat like regular conversation text

## Structured Data (reports, handovers, tables)
When messages contain structured/tabular content such as work handover reports, status summaries, or key-value lists:
- Use the task entity type for ticket/issue items. Store ticket IDs in properties (e.g., {"ticket_id": "#942681"})
- Store numeric counts in properties (e.g., {"open_tickets": "0", "resolved_tickets": "4"})
- Store status values in properties (e.g., {"status": "on hold"})
- Store reference numbers and client/organization context in properties (e.g., {"ref": "2022338375", "client": "BANK MESTIKA DHARMA"})
- Extract individual ticket line items as separate task entities when detail is available
- For the overall report/summary, use document entity type with properties like {"report_type": "work handover"}
- Convert date formats like "21 April 2026", "17-04-2026", or "20.00 WIB" to ISO 8601 (e.g., "2026-04-21T20:00:00+07:00")
`

// listenSummarizePrompt summarizes WhatsApp conversations into polished text
// while preserving specific details (names, IDs, timestamps, structured data)
// needed for accurate KG extraction.
const listenSummarizePrompt = `Summarize this WhatsApp conversation concisely. Focus on substantive content and organize by topic.

## CRITICAL: Preserve These Details Exactly
- Full names of all people mentioned (do not abbreviate or generalize)
- Ticket IDs, reference numbers, issue numbers (e.g., #942681, 2022338375)
- Exact dates, times, and deadlines (preserve the original format and timezone)
- Client/organization names exactly as stated (e.g., "BANK MESTIKA DHARMA", not "a bank")
- Numeric counts and statistics (e.g., "3 on-hold tickets", "0 open tickets")
- Status values (e.g., "on hold", "in progress", "closed")
- Technology names, versions, and configurations
- Project names and task descriptions

## Structured Data
When the messages contain structured/tabular content (work handovers, status reports, ticket lists):
- Preserve the structured data in a clear format (bulleted list or table)
- Keep ticket IDs, reference numbers, client names, and descriptions together
- Keep numeric summaries (open/on-hold/closed counts) intact
- Do NOT summarize away individual ticket details into generic statements

## What to Include
- Key decisions and action items with owners
- Technical details: tools, systems, configurations discussed
- Relationships between people, projects, and tasks
- Deadlines and scheduled events with exact dates/times
- Problem descriptions and resolution status
- Work assignments and responsibilities

## What to Omit
- Greetings, goodbyes, filler words
- Emoji reactions and acknowledgments (ok, thanks, noted) unless they signal a decision
- Repeated information (state once with attribution)

## Output
2-4 paragraphs of polished summary, organized by topic. Include an entity-rich narrative where every person, project, ticket, and deadline is named explicitly. Use the same language as the original messages.`
