package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := os.Getenv("GOCLAW_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("GOCLAW_POSTGRES_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	graphID := "project-sovereign"

	// 1. Raw messages May 8 with delivery/status
	fmt.Println("=== Raw messages May 8 with 'delivery' or 'status' ===")
	rows, _ := db.Query(`SELECT msg_timestamp, sender, LEFT(body, 150) FROM listen_raw_messages WHERE graph_id = $1 AND msg_timestamp >= '2026-05-08' AND msg_timestamp < '2026-05-09' AND (body ILIKE '%deliver%' OR body ILIKE '%status%') ORDER BY msg_timestamp`, graphID)
	defer rows.Close()
	var cnt int
	for rows.Next() {
		var ts, sender, body string
		rows.Scan(&ts, &sender, &body)
		cnt++
		fmt.Printf("  [%s] %s: %s\n", ts[:16], sender, body)
	}
	fmt.Printf("Found: %d\n", cnt)

	// 2. ALL chunks for project-sovereign with time ranges
	fmt.Println("\n=== All chunks (time ranges + overlap analysis) ===")
	type chunkInfo struct {
		idx      int
		from     string
		to       string
		textLen  int
		agentID  string
	}
	rows2, _ := db.Query(`SELECT agent_id, chunk_index, msg_time_from::text, msg_time_to::text, LENGTH(text) FROM raw_message_chunks WHERE graph_id = $1 ORDER BY agent_id, chunk_index`, graphID)
	defer rows2.Close()
	var chunks []chunkInfo
	for rows2.Next() {
		var c chunkInfo
		rows2.Scan(&c.agentID, &c.idx, &c.from, &c.to, &c.textLen)
		chunks = append(chunks, c)
	}

	for _, c := range chunks {
		contains := ""
		if c.from < "2026-05-09" && c.to >= "2026-05-08" {
			contains = " <-- MAY 8"
		}
		fmt.Printf("  agent=%.8s chunk %2d: %s → %s (%5d chars)%s\n", c.agentID, c.idx, c.from[:16], c.to[:16], c.textLen, contains)
	}

	// 3. FTS search: "status delivery 8th may"
	queries := []string{
		"status delivery 8 may",
		"delivery may 8",
		"handover 8 may",
		"WORK HANDOVER INFORMATION 08 Mei",
		"delivery Private cloud KAI",
	}
	for _, q := range queries {
		fmt.Printf("\n=== FTS: '%s' ===\n", q)
		rows3, _ := db.Query(`SELECT agent_id, chunk_index, msg_time_from::text, msg_time_to::text, ts_rank(tsv, plainto_tsquery('simple', $1)) AS score, LEFT(text, 100) FROM raw_message_chunks WHERE graph_id = $2 AND tsv @@ plainto_tsquery('simple', $1) ORDER BY score DESC LIMIT 3`, q, graphID)
		defer rows3.Close()
		var found int
		for rows3.Next() {
			var aid, from, to, preview string
			var idx int
			var score float64
			rows3.Scan(&aid, &idx, &from, &to, &score, &preview)
			found++
			fmt.Printf("  agent=%.8s chunk %d score=%.4f %s→%s\n    preview: %s\n", aid, idx, score, from[:10], to[:10], preview)
		}
		if found == 0 {
			fmt.Println("  NO RESULTS")
		}
	}

	// 4. Check if text is actually in chunks
	fmt.Println("\n=== Direct text search in chunks: 'delivery' on May 8 ===")
	rows4, _ := db.Query(`SELECT agent_id, chunk_index, msg_time_from::text, msg_time_to::text FROM raw_message_chunks WHERE graph_id = $1 AND text ILIKE '%deliver%' AND msg_time_to >= '2026-05-08' AND msg_time_from <= '2026-05-09'`, graphID)
	defer rows4.Close()
	for rows4.Next() {
		var aid, from, to string
		var idx int
		rows4.Scan(&aid, &idx, &from, &to)
		fmt.Printf("  agent=%.8s chunk %d: %s → %s\n", aid, idx, from[:10], to[:10])
	}

	// 5. Check what's in chunks for May 8
	fmt.Println("\n=== Chunks covering May 8 - handover content check ===")
	rows5, _ := db.Query(`SELECT agent_id, chunk_index, msg_time_from::text, msg_time_to::text, LENGTH(text), CASE WHEN text ILIKE '%handover%' THEN 'Y' ELSE 'N' END, CASE WHEN text ILIKE '%deliver%' THEN 'Y' ELSE 'N' END, CASE WHEN text ILIKE '%08 Mei%' THEN 'Y' ELSE 'N' END, CASE WHEN text ILIKE '%May 2026%08%' THEN 'Y' ELSE 'N' END FROM raw_message_chunks WHERE graph_id = $1 AND msg_time_to >= '2026-05-08' AND msg_time_from <= '2026-05-09' ORDER BY agent_id, chunk_index`, graphID)
	defer rows5.Close()
	fmt.Printf("%-10s %5s %10s %10s %6s %8s %8s %8s %8s\n", "agent", "chunk", "from", "to", "chars", "handover", "deliver", "08Mei", "May08")
	fmt.Println(strings.Repeat("-", 80))
	for rows5.Next() {
		var aid, from, to string
		var idx, textLen int
		var hasHO, hasDel, has08Mei, hasMay08 string
		rows5.Scan(&aid, &idx, &from, &to, &textLen, &hasHO, &hasDel, &has08Mei, &hasMay08)
		fmt.Printf("%.8s %5d %10s %10s %6d %8s %8s %8s %8s\n", aid, idx, from[:10], to[:10], textLen, hasHO, hasDel, has08Mei, hasMay08)
	}

	// 6. Show the actual May 8 handover text that should be findable
	fmt.Println("\n=== Sample May 8 handover content from raw messages ===")
	rows6, _ := db.Query(`SELECT msg_timestamp, sender, LEFT(body, 300) FROM listen_raw_messages WHERE graph_id = $1 AND msg_timestamp >= '2026-05-08' AND msg_timestamp < '2026-05-09' AND body ILIKE '%handover%' ORDER BY msg_timestamp LIMIT 3`, graphID)
	defer rows6.Close()
	for rows6.Next() {
		var ts, sender, body string
		rows6.Scan(&ts, &sender, &body)
		fmt.Printf("--- [%s] %s ---\n%s\n\n", ts[:16], sender, body)
	}
}
