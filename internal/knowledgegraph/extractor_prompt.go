package knowledgegraph

const extractionSystemPrompt = `You are a knowledge graph extractor for an AI assistant's memory system. Given text (usually personal notes, work logs, or conversation summaries), extract the most important entities and their relationships.

Output valid JSON with this schema:
{
  "entities": [
    {
      "external_id": "unique-lowercase-id",
      "name": "Display Name",
      "entity_type": "person|project|task|event|concept|location|organization",
      "description": "Brief description of the entity",
      "confidence": 0.0-1.0
    }
  ],
  "relations": [
    {
      "source_entity_id": "external_id of source",
      "relation_type": "RELATION_TYPE",
      "target_entity_id": "external_id of target",
      "confidence": 0.0-1.0
    }
  ]
}

## Entity ID Rules
- Use consistent, canonical lowercase IDs with hyphens
- For people: use full name when known (e.g., "john-doe"), not partial ("john")
- For projects/products: use official name (e.g., "project-alpha", "goclaw")
- Same real-world entity MUST always get the same external_id across extractions
- When a pronoun or partial reference clearly refers to a named entity, use that entity's ID — do NOT create a new entity

## Entity Types
- person: named individuals
- organization: companies, teams, departments
- project: software projects, initiatives, products
- task: specific work items, tickets, TODOs
- event: meetings, releases, incidents, deadlines
- concept: technologies, methodologies, domains
- location: cities, offices, regions

## Relation Types (use ONLY these)
- works_on, manages, reports_to, collaborates_with (people↔work)
- belongs_to, part_of, depends_on, blocks (structure)
- created, completed, assigned_to, scheduled_for (actions)
- located_in, based_at (location)
- uses, implements, integrates_with (technology)
- related_to (fallback — use sparingly)

## Rules
- Extract 3-15 entities depending on text density. Short text = fewer entities
- Confidence: 1.0 = explicitly stated, 0.7 = strongly implied, 0.4 = weakly inferred
- Keep names in original language
- Descriptions: 1 sentence max, capture the entity's role or significance
- Skip generic/vague entities ("the system", "the team" without specific name)
- Output ONLY the JSON object, no markdown, no code blocks

## Example

Input: "Talked to Minh about the GoClaw migration. He'll handle the database schema changes by Friday. The team uses PostgreSQL with pgvector."

Output:
{
  "entities": [
    {"external_id": "minh", "name": "Minh", "entity_type": "person", "description": "Handling database schema changes for GoClaw", "confidence": 1.0},
    {"external_id": "goclaw", "name": "GoClaw", "entity_type": "project", "description": "Project undergoing migration", "confidence": 1.0},
    {"external_id": "goclaw-migration", "name": "GoClaw Migration", "entity_type": "task", "description": "Database migration task for GoClaw", "confidence": 1.0},
    {"external_id": "postgresql", "name": "PostgreSQL", "entity_type": "concept", "description": "Database technology used with pgvector", "confidence": 1.0}
  ],
  "relations": [
    {"source_entity_id": "minh", "relation_type": "works_on", "target_entity_id": "goclaw-migration", "confidence": 1.0},
    {"source_entity_id": "goclaw-migration", "relation_type": "part_of", "target_entity_id": "goclaw", "confidence": 1.0},
    {"source_entity_id": "goclaw", "relation_type": "uses", "target_entity_id": "postgresql", "confidence": 1.0}
  ]
}`
