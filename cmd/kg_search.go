package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func kgSearchCmd() *cobra.Command {
	var (
		agentID  string
		tenantID string
		scope    string
		scopes   []string
		limit    int
		entityID string
		depth    int
	)

	cmd := &cobra.Command{
		Use:   "kg-search [query]",
		Short: "Test knowledge graph search queries",
		Long:  "Directly query the knowledge graph store to verify search results.\nUses GOCLAW_POSTGRES_DSN from environment.",
		Example: `  # Search for GPU-related entities
  goclaw kg-search "openrouter GPU" --agent-id 019d6771-abce-7ad1-8e4d-8ee0a211c3cc --tenant 0193a5b0-7000-7000-8000-000000000001 --scopes project-gpu-elephant,project-sovereign

  # Traverse from a specific entity
  goclaw kg-search --entity-id 019dd6dd-e892-7622-84ee-4a51af84613f --agent-id 019d6771-abce-7ad1-8e4d-8ee0a211c3cc --tenant 0193a5b0-7000-7000-8000-000000000001 --depth 2`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			dsn := os.Getenv("GOCLAW_POSTGRES_DSN")
			if dsn == "" {
				fmt.Fprintln(os.Stderr, "GOCLAW_POSTGRES_DSN not set")
				os.Exit(1)
			}
			if agentID == "" {
				fmt.Fprintln(os.Stderr, "--agent-id is required")
				os.Exit(1)
			}
			if tenantID == "" {
				fmt.Fprintln(os.Stderr, "--tenant is required")
				os.Exit(1)
			}
			aid, parseErr := uuid.Parse(agentID)
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "Invalid agent ID: %v\n", parseErr)
				os.Exit(1)
			}
			tid, parseErr := uuid.Parse(tenantID)
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "Invalid tenant ID: %v\n", parseErr)
				os.Exit(1)
			}

			db, err := sql.Open("pgx", dsn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "DB open: %v\n", err)
				os.Exit(1)
			}
			defer db.Close()
			pg.InitSqlx(db)

			kgStore := pg.NewPGKnowledgeGraphStore(db)

			// Build context with tenant + shared KG IDs
			ctx := context.Background()
			ctx = store.WithAgentID(ctx, aid)
			ctx = store.WithTenantID(ctx, tid)
			ctx = store.WithSharedKG(ctx)
			if len(scopes) > 0 {
				ctx = store.WithSharedKGIDs(ctx, scopes)
			}

			var query string
			if len(args) > 0 {
				query = strings.Join(args, " ")
			}

			fmt.Printf("Agent: %s\n", agentID)
			fmt.Printf("Tenant: %s\n", tenantID)
			if len(scopes) > 0 {
				fmt.Printf("Scopes: %v\n", scopes)
			}
			fmt.Println(strings.Repeat("─", 60))

			// Stats
			userID := ""
			if scope != "" {
				userID = scope
			}
			stats, statsErr := kgStore.Stats(ctx, agentID, userID)
			if statsErr != nil {
				fmt.Fprintf(os.Stderr, "Stats ERROR: %v\n", statsErr)
			} else {
				fmt.Printf("KG Stats: %d entities, %d relations\n\n", stats.EntityCount, stats.RelationCount)
			}

			// Traversal mode
			if entityID != "" {
				fmt.Printf("[Traverse] entity_id=%q depth=%d\n\n", entityID, depth)
				results, travErr := kgStore.Traverse(ctx, agentID, userID, entityID, depth)
				if travErr != nil {
					fmt.Fprintf(os.Stderr, "Traverse ERROR: %v\n", travErr)
				} else {
					fmt.Printf("Found %d reachable entities:\n", len(results))
					for i, r := range results {
						if i >= limit {
							fmt.Printf("  ... (+%d more)\n", len(results)-limit)
							break
						}
						fmt.Printf("\n%d. [depth %d] %s [%s] (id: %s)\n", i+1, r.Depth, r.Entity.Name, r.Entity.EntityType, r.Entity.ID)
						if r.Entity.Description != "" {
							fmt.Printf("   %s\n", r.Entity.Description)
						}
						if len(r.Entity.Properties) > 0 {
							fmt.Printf("   [%s]\n", formatKGProps(r.Entity.Properties))
						}
						if r.Entity.EventTime != nil {
							fmt.Printf("   event: %s\n", r.Entity.EventTime.Format("2006-01-02 15:04"))
						}
						if r.Via != "" {
							fmt.Printf("   via: %s\n", r.Via)
						}
					}
				}
				return
			}

			// Search mode
			if query == "" {
				fmt.Fprintln(os.Stderr, "Provide a query argument or use --entity-id for traversal")
				os.Exit(1)
			}

			fmt.Printf("[SearchEntities] query=%q limit=%d\n\n", query, limit)
			entities, err := kgStore.SearchEntities(ctx, agentID, userID, query, limit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Search ERROR: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Found %d entities:\n", len(entities))
			for i, e := range entities {
				fmt.Printf("\n%d. %s [%s] (id: %s)\n", i+1, e.Name, e.EntityType, e.ID)
				if e.Description != "" {
					fmt.Printf("   %s\n", e.Description)
				}
				if len(e.Properties) > 0 {
					fmt.Printf("   [%s]\n", formatKGProps(e.Properties))
				}
				if e.EventTime != nil {
					fmt.Printf("   event: %s\n", e.EventTime.Format("2006-01-02 15:04"))
				}
				fmt.Printf("   user_id: %s\n", e.UserID)

				// Show relations
				rels, relErr := kgStore.ListRelations(ctx, agentID, userID, e.ID)
				if relErr == nil && len(rels) > 0 {
					fmt.Printf("   Relations (%d):\n", len(rels))
					for _, r := range rels {
						fmt.Printf("     %s —[%s]→ %s\n", r.SourceEntityID[:8], r.RelationType, r.TargetEntityID[:8])
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID (required)")
	cmd.Flags().StringVar(&tenantID, "tenant", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "Single scope ID (user_id filter)")
	cmd.Flags().StringSliceVar(&scopes, "scopes", nil, "Shared KG scope IDs")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results")
	cmd.Flags().StringVar(&entityID, "entity-id", "", "Entity ID to traverse from")
	cmd.Flags().IntVar(&depth, "depth", 3, "Traversal depth (default 3)")

	return cmd
}

func formatKGProps(props map[string]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + props[k]
	}
	return strings.Join(parts, ", ")
}
