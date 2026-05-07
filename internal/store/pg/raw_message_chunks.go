package pg

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGRawMessageChunkStore implements store.RawMessageChunkStore backed by Postgres.
type PGRawMessageChunkStore struct {
	db       *sql.DB
	provider store.EmbeddingProvider
}

func NewPGRawMessageChunkStore(db *sql.DB) *PGRawMessageChunkStore {
	return &PGRawMessageChunkStore{db: db}
}

func (s *PGRawMessageChunkStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.provider = provider
}

// GetEmbeddingProvider returns the configured embedding provider.
func (s *PGRawMessageChunkStore) GetEmbeddingProvider() (store.EmbeddingProvider, bool) {
	return s.provider, s.provider != nil
}

// StoreChunks persists chunks with their embeddings in a single batch INSERT.
func (s *PGRawMessageChunkStore) StoreChunks(ctx context.Context, chunks []store.RawMessageChunk, embeddings [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}

	now := time.Now()
	tid := tenantIDForInsert(ctx)

	const cols = 17
	placeholders := make([]string, len(chunks))
	args := make([]any, 0, len(chunks)*cols)

	for i, c := range chunks {
		id := c.ID
		if id == "" {
			id = uuid.Must(uuid.NewV7()).String()
		}
		base := i * cols

		var embArg any
		hasEmb := i < len(embeddings) && len(embeddings[i]) > 0
		if hasEmb {
			embArg = vectorToString(embeddings[i])
		}

		msgIDs := pq.Array(c.SourceMsgIDs)

		embPH := fmt.Sprintf("$%d", base+13)
		if hasEmb {
			embPH += "::vector"
		}

		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,%s,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10,
			base+11, base+12, embPH, base+14, base+15, base+16, base+17)

		args = append(args, id, c.AgentID, c.GraphID, c.ChatID, c.ChatName,
			c.Sender, c.SenderID, c.MsgTimeFrom, c.MsgTimeTo,
			c.ChunkIndex, c.Text, c.ContentHash,
			embArg, msgIDs, tid, now, now)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO raw_message_chunks
			(id, agent_id, graph_id, chat_id, chat_name, sender, sender_id,
			 msg_time_from, msg_time_to, chunk_index, text, content_hash,
			 embedding, source_msg_ids, tenant_id, created_at, updated_at)
		 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

// scoredRawChunk is an intermediate result from FTS or vector search.
type scoredRawChunk struct {
	id        string
	agentID   string
	graphID   string
	chatID    string
	chatName  string
	sender    string
	senderID  string
	timeFrom  time.Time
	timeTo    time.Time
	chunkIdx  int
	text      string
	score     float64
	sourceIDs []string
}

// Search performs hybrid FTS + vector search with Reciprocal Rank Fusion (RRF).
func (s *PGRawMessageChunkStore) Search(ctx context.Context, query, agentID string, opts store.RawMessageChunkSearchOptions) ([]store.RawMessageChunkSearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	rrfK := opts.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}

	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("raw message chunk search: %w", err)
	}

	ftsResults, err := s.ftsSearch(ctx, query, aid, opts, maxResults*2)
	if err != nil {
		return nil, err
	}

	var vecResults []scoredRawChunk
	if s.provider != nil {
		embeddings, embErr := s.provider.Embed(ctx, []string{query})
		if embErr == nil && len(embeddings) > 0 {
			vecResults, err = s.vectorSearch(ctx, embeddings[0], aid, opts, maxResults*2)
			if err != nil {
				vecResults = nil
			}
		}
	}

	merged := rrfMerge(ftsResults, vecResults, rrfK)

	var filtered []store.RawMessageChunkSearchResult
	for _, m := range merged {
		if opts.MinScore > 0 && m.Score < opts.MinScore {
			continue
		}
		filtered = append(filtered, m)
		if len(filtered) >= maxResults {
			break
		}
	}
	return filtered, nil
}

func (s *PGRawMessageChunkStore) ftsSearch(ctx context.Context, query string, agentID uuid.UUID, opts store.RawMessageChunkSearchOptions, limit int) ([]scoredRawChunk, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 5)
	if err != nil {
		return nil, err
	}

	args := []any{query, agentID, query}
	paramIdx := 4

	var extraWhere string
	if opts.ChatID != "" {
		extraWhere += fmt.Sprintf(" AND chat_id = $%d", paramIdx)
		args = append(args, opts.ChatID)
		paramIdx++
	}
	if opts.GraphID != "" {
		extraWhere += fmt.Sprintf(" AND graph_id = $%d", paramIdx)
		args = append(args, opts.GraphID)
		paramIdx++
	}
	if opts.FromTime != nil {
		extraWhere += fmt.Sprintf(" AND msg_time_from >= $%d", paramIdx)
		args = append(args, *opts.FromTime)
		paramIdx++
	}
	if opts.ToTime != nil {
		extraWhere += fmt.Sprintf(" AND msg_time_to <= $%d", paramIdx)
		args = append(args, *opts.ToTime)
		paramIdx++
	}

	limitN := paramIdx + len(tcArgs)
	q := fmt.Sprintf(`SELECT id, agent_id, graph_id, chat_id, chat_name, sender, sender_id,
			msg_time_from, msg_time_to, chunk_index, text, source_msg_ids,
			ts_rank(tsv, plainto_tsquery('simple', $1)) AS score
		FROM raw_message_chunks
		WHERE agent_id = $2 AND tsv @@ plainto_tsquery('simple', $3)%s%s
		ORDER BY score DESC LIMIT $%d`, extraWhere, tc, limitN)

	args = append(args, tcArgs...)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredRawChunk
	for rows.Next() {
		var r scoredRawChunk
		if err := rows.Scan(&r.id, &r.agentID, &r.graphID, &r.chatID, &r.chatName,
			&r.sender, &r.senderID, &r.timeFrom, &r.timeTo, &r.chunkIdx, &r.text,
			pq.Array(&r.sourceIDs), &r.score); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *PGRawMessageChunkStore) vectorSearch(ctx context.Context, embedding []float32, agentID uuid.UUID, opts store.RawMessageChunkSearchOptions, limit int) ([]scoredRawChunk, error) {
	vecStr := vectorToString(embedding)

	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return nil, err
	}

	args := []any{vecStr, agentID}
	paramIdx := 3

	var extraWhere string
	if opts.ChatID != "" {
		extraWhere += fmt.Sprintf(" AND chat_id = $%d", paramIdx)
		args = append(args, opts.ChatID)
		paramIdx++
	}
	if opts.GraphID != "" {
		extraWhere += fmt.Sprintf(" AND graph_id = $%d", paramIdx)
		args = append(args, opts.GraphID)
		paramIdx++
	}
	if opts.FromTime != nil {
		extraWhere += fmt.Sprintf(" AND msg_time_from >= $%d", paramIdx)
		args = append(args, *opts.FromTime)
		paramIdx++
	}
	if opts.ToTime != nil {
		extraWhere += fmt.Sprintf(" AND msg_time_to <= $%d", paramIdx)
		args = append(args, *opts.ToTime)
		paramIdx++
	}

	orderN := paramIdx + len(tcArgs)
	limitN := orderN + 1
	q := fmt.Sprintf(`SELECT id, agent_id, graph_id, chat_id, chat_name, sender, sender_id,
			msg_time_from, msg_time_to, chunk_index, text, source_msg_ids,
			1 - (embedding <=> $1::vector) AS score
		FROM raw_message_chunks
		WHERE agent_id = $2 AND embedding IS NOT NULL%s%s
		ORDER BY embedding <=> $%d::vector LIMIT $%d`, extraWhere, tc, orderN, limitN)

	args = append(args, tcArgs...)
	args = append(args, vecStr, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredRawChunk
	for rows.Next() {
		var r scoredRawChunk
		if err := rows.Scan(&r.id, &r.agentID, &r.graphID, &r.chatID, &r.chatName,
			&r.sender, &r.senderID, &r.timeFrom, &r.timeTo, &r.chunkIdx, &r.text,
			pq.Array(&r.sourceIDs), &r.score); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// rrfMerge combines FTS and vector results using Reciprocal Rank Fusion.
// Score = Σ 1/(k + rank) for each list the chunk appears in.
func rrfMerge(fts, vec []scoredRawChunk, k int) []store.RawMessageChunkSearchResult {
	type entry struct {
		result  store.RawMessageChunkSearchResult
		ftsRank int
		vecRank int
	}
	seen := make(map[string]*entry)

	for rank, r := range fts {
		seen[r.id] = &entry{
			result: store.RawMessageChunkSearchResult{
				Chunk: store.RawMessageChunk{
					ID: r.id, AgentID: r.agentID, GraphID: r.graphID,
					ChatID: r.chatID, ChatName: r.chatName,
					Sender: r.sender, SenderID: r.senderID,
					MsgTimeFrom: r.timeFrom, MsgTimeTo: r.timeTo,
					ChunkIndex: r.chunkIdx, Text: r.text,
					SourceMsgIDs: r.sourceIDs,
				},
				Score:   1.0 / float64(k+rank+1),
				FTSRank: rank + 1,
			},
		}
	}

	for rank, r := range vec {
		if e, ok := seen[r.id]; ok {
			e.result.Score += 1.0 / float64(k+rank+1)
			e.result.VectorRank = rank + 1
		} else {
			seen[r.id] = &entry{
				result: store.RawMessageChunkSearchResult{
					Chunk: store.RawMessageChunk{
						ID: r.id, AgentID: r.agentID, GraphID: r.graphID,
						ChatID: r.chatID, ChatName: r.chatName,
						Sender: r.sender, SenderID: r.senderID,
						MsgTimeFrom: r.timeFrom, MsgTimeTo: r.timeTo,
						ChunkIndex: r.chunkIdx, Text: r.text,
						SourceMsgIDs: r.sourceIDs,
					},
					Score:      1.0 / float64(k+rank+1),
					VectorRank: rank + 1,
				},
			}
		}
	}

	results := make([]store.RawMessageChunkSearchResult, 0, len(seen))
	for _, e := range seen {
		results = append(results, e.result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// DeleteByGraphID removes all chunks for a given agent+graph scope.
func (s *PGRawMessageChunkStore) DeleteByGraphID(ctx context.Context, agentID, graphID string) error {
	tc, tcArgs, _, err := scopeClause(ctx, 3)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM raw_message_chunks WHERE agent_id = $1 AND graph_id = $2`+tc,
		append([]any{agentID, graphID}, tcArgs...)...,
	)
	return err
}
