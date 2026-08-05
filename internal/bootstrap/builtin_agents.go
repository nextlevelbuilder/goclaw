package bootstrap

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/google/uuid"
)

// BuiltinAgent is a format specialist that EVERY user of a tenant sees, without
// anything being copied into their account.
//
// WHY BUILT-IN RATHER THAN SEEDED PER USER
//
// The starter templates in internal/http are cloned into a personal agent on a
// user's first agents.list, but only when they own no agents yet. That guard
// exists so a starter someone deleted is not resurrected — and it means a
// template added later never reaches an existing user. Adding these four to that
// list would have shipped four agents that nobody already using the product
// could see.
//
// A built-in is one row per TENANT: owner_id='system', agent_type='predefined',
// is_locked=true. ListAccessible already returns exactly that shape to every
// member ("agent_type = 'predefined' AND owner_id = 'system'"), which is how the
// original researcher/writer/coder were visible before they moved per-user.
//
// THE TRADEOFF, stated plainly: a built-in's prompt is SHARED, so one user
// editing it would change it for their team-mates — which is precisely why
// migration 000066 moved researcher/writer/coder per-user. is_locked closes that
// door instead: nobody edits a built-in, you clone it if you want a variant. That
// is the right trade for a FORMAT specialist (there is one good way to build a
// deck) and the wrong one for a role (my researcher should sound like me).
type BuiltinAgent struct {
	Key          string
	DisplayName  string
	Emoji        string
	SystemPrompt string
	MaxIter      int
}

// Provider and model match the per-user starters in internal/http rather than
// config defaults, so a built-in and a cloned starter behave the same way on a
// fresh workspace.
const (
	builtinProvider = "llm-service"
	builtinModel    = "gemini-3.5-flash"
)

// BuiltinAgents are the deliverable formats: a deck, a spreadsheet, a document,
// and images/video.
//
// Every skill named below is one we ship (skills/: pptx, xlsx, sheet-bulk-enrich,
// docx, pdf) and every tool name is registered in internal/tools. A prompt naming
// a capability we do not have costs nothing at compile time and then makes the
// agent hunt for something it cannot load — builtin_agents_test.go pins both.
//
// Google Slides/Docs/Sheets are offered CONDITIONALLY, because they depend on a
// Composio connection and on per-agent access granted from the Integrations page.
// Each prompt therefore leads with producing a real file through a skill, which
// works with nothing connected at all.
var BuiltinAgents = []BuiltinAgent{
	{
		Key:         "slides",
		DisplayName: "Slides",
		Emoji:       "📊",
		SystemPrompt: `You build presentations.

Default to producing a real file: use skill_search to find the "pptx" skill, use_skill to load it, then build the deck and hand it over with deliver_file. If the workspace has Google Slides connected, offer that instead when the user wants something collaborative — ask which they want if it is not obvious.

Craft, not decoration:
- One idea per slide. If a slide needs two claims, it is two slides.
- Titles that state the point ("Churn doubled in Q3"), not the topic ("Churn").
- Speaker notes carry the argument; the slide carries the evidence.
- Six lines of text on a slide is a document, not a slide.

Ask for the audience and the length before building anything longer than five slides — a board deck and an all-hands deck about the same numbers are different decks. If you need facts you do not have, use web_search and cite them on the slide itself.`,
		MaxIter: 100,
	},
	{
		Key:         "sheets",
		DisplayName: "Sheets",
		Emoji:       "🧮",
		SystemPrompt: `You build and analyse spreadsheets.

Use skill_search / use_skill to load the "xlsx" skill for building files, and "sheet-bulk-enrich" when a column has to be filled in per row from research or an API. Hand finished files over with deliver_file. If the workspace has Google Sheets connected, that is the better home for anything the user will keep editing.

How to think about a sheet:
- One row per record, one column per attribute, one header row, no merged cells.
- Formulas over baked-in values, so the sheet stays true when inputs change.
- State units and currency in the header, not in each cell.
- Never invent a number. An empty cell with a note beats a plausible guess.

When asked to analyse, say what the data shows AND what it cannot show — sample size, missing rows, a date range that does not cover the question. Show the formula you used so the result can be checked.`,
		MaxIter: 100,
	},
	{
		Key:         "docs",
		DisplayName: "Docs",
		Emoji:       "📄",
		SystemPrompt: `You produce documents — reports, specs, memos, briefs.

Use skill_search / use_skill to load the "docx" skill for Word output or "pdf" for a fixed-layout deliverable, then deliver_file. If the workspace has Google Docs connected, prefer that for anything that will be reviewed or commented on.

This is the Writer's sibling: the Writer works on prose, you work on DOCUMENTS — structure, headings, tables, a contents list when it runs long, consistent terminology throughout.

- Lead with the conclusion. A reader who stops after the first paragraph should still have the answer.
- Headings that a stranger can navigate by.
- Say who the document is for and what it decides, near the top.
- Mark what is assumed and what is established; do not blur them.

Ask what format is wanted before writing something long — a docx that should have been a PDF is a wasted round trip.`,
		MaxIter: 100,
	},
	{
		Key:         "studio",
		DisplayName: "Studio",
		Emoji:       "🎬",
		SystemPrompt: `You make images and video.

create_image for stills, create_video for motion. Deliver results with deliver_file when the user wants the file itself.

Prompting is your craft, and the user's request is usually not yet a prompt:
- Pin down subject, style, composition and aspect ratio before generating. "A logo" is not a brief.
- State what to EXCLUDE when it matters — an empty background, no text, no people.
- Video is expensive and slow compared to a still. For anything uncertain, generate one image first to agree the look, then animate it.
- Show one option and ask, rather than burning four generations on a guess.

Say plainly what these models are bad at (legible text in images, precise counts, a consistent character across separate generations) instead of trying repeatedly and quietly failing. If a request needs exact text or brand assets, say so and suggest composing it over a generated background.`,
		MaxIter: 100,
	},
}

// EnsureBuiltinAgents creates any missing built-in for every tenant. Idempotent:
// the WHERE NOT EXISTS is backed by the (tenant_id, agent_key) unique index, so a
// concurrent second gateway cannot double-insert.
//
// Runs at startup, like BackfillCapabilities. A tenant created afterwards gets
// its built-ins on the next start; they are not required for the tenant to
// function, so blocking tenant creation on them would trade a real failure mode
// for a cosmetic one.
//
// workspaceRoot is the same path the per-user starters use, with the key as the
// leaf — each built-in gets its own directory rather than sharing one, so a file
// Slides writes cannot collide with a file Docs writes.
// EnsureBuiltinAgentsForTenant is the same guarantee for ONE tenant.
//
// The startup pass covers every tenant that existed when the gateway booted. A
// tenant created afterwards — which is what a brand-new organisation is — would
// otherwise have no built-ins until the next restart, i.e. until the next deploy.
// Callers on a request path use this so a new org gets them on first use.
func EnsureBuiltinAgentsForTenant(ctx context.Context, db *sql.DB, workspaceRoot string, tenantID uuid.UUID) (int64, error) {
	return ensureBuiltins(ctx, db, workspaceRoot, &tenantID)
}

func EnsureBuiltinAgents(ctx context.Context, db *sql.DB, workspaceRoot string) (int64, error) {
	return ensureBuiltins(ctx, db, workspaceRoot, nil)
}

func ensureBuiltins(ctx context.Context, db *sql.DB, workspaceRoot string, tenantID *uuid.UUID) (int64, error) {
	// PostgreSQL only, and deliberately a no-op elsewhere: the sqliteonly build
	// leaves Stores.DB nil, so desktop/Lite skips this the same way it skips
	// BackfillCapabilities — which uses the same uuid_generate_v7()/NOW(). Lite
	// also caps a workspace at five agents, so four built-ins would eat most of
	// the allowance for something a single-user desktop does not need.
	if db == nil {
		return 0, nil
	}

	// A nil tenant means "every tenant"; the SQL treats NULL as no filter rather
	// than needing two versions of the statement.
	var tenantArg any
	if tenantID != nil {
		tenantArg = *tenantID
	}

	var total int64
	for _, a := range BuiltinAgents {
		// The casts are NOT decoration. $1 appears twice — as the inserted
		// agent_key and in the NOT EXISTS comparison — and Postgres refuses to
		// deduce one type for both uses:
		//   ERROR: inconsistent types deduced for parameter $1 (SQLSTATE 42P08)
		// which is exactly how this failed on staging: the statement never ran, the
		// warning was logged, and nothing else broke. Every parameter is cast so
		// the next one added does not reintroduce it.
		res, err := db.ExecContext(ctx, `
			INSERT INTO agents (
				id, agent_key, display_name, emoji, owner_id, tenant_id,
				agent_type, provider, model, status, workspace,
				restrict_to_workspace, system_prompt, max_tool_iterations,
				is_locked, is_default, memory_config, compaction_config,
				other_config, created_at, updated_at
			)
			SELECT
				uuid_generate_v7(), $1::varchar, $2::varchar, $3::text, 'system', t.id,
				'predefined', $4::varchar, $5::varchar, 'active', $6::text,
				true, $7::text, $8::int,
				true, false, '{"enabled":true}', '{}',
				'{"bootstrapMaxChars":24000}', NOW(), NOW()
			FROM tenants t
			WHERE ($9::uuid IS NULL OR t.id = $9::uuid)
			AND NOT EXISTS (
				SELECT 1 FROM agents ex
				WHERE ex.tenant_id = t.id AND ex.agent_key = $1::varchar AND ex.deleted_at IS NULL
			)`,
			a.Key, a.DisplayName, a.Emoji,
			builtinProvider, builtinModel, workspaceRoot+"/"+a.Key,
			a.SystemPrompt, a.MaxIter, tenantArg,
		)
		if err != nil {
			// Keep going: one built-in failing should not deny the others. The
			// caller logs, and the next start retries.
			return total, err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			slog.Info("bootstrap: built-in agent created", "agent_key", a.Key, "tenants", n)
		}
		total += n
	}
	return total, nil
}
