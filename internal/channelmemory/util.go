package channelmemory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *Service) itemFromExtracted(run *store.ChannelMemoryExtractionRun, extracted ExtractedItem) *store.ChannelMemoryExtractionItem {
	topics, _ := json.Marshal(extracted.Topics)
	entities, _ := json.Marshal(extracted.Entities)
	hash := sha256.Sum256([]byte(strings.Join([]string{run.ChannelInstanceID.String(), run.HistoryKey, extracted.Type, extracted.Summary}, "\x00")))
	itemID := hex.EncodeToString(hash[:])
	return &store.ChannelMemoryExtractionItem{
		RunID:             run.ID,
		ChannelInstanceID: run.ChannelInstanceID,
		AgentID:           run.AgentID,
		UserID:            run.UserID,
		ItemHash:          itemID,
		ItemType:          extracted.Type,
		Summary:           extracted.Summary,
		Topics:            topics,
		Entities:          entities,
		Confidence:        extracted.Confidence,
		SourceID:          "channel:" + itemID,
	}
}

func eligibleHistoryKey(key string, cfg Config) bool {
	if !cfg.GroupOnly {
		return key != ""
	}
	k := strings.ToLower(key)
	return key != "" && !strings.Contains(k, "dm") && !strings.Contains(k, "private")
}

func messageSourceID(msg store.PendingMessage) string {
	if msg.PlatformMsgID != "" {
		return msg.PlatformMsgID
	}
	return msg.ID.String()
}

func decodeStrings(raw json.RawMessage) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

//go:fix inline
func timePtr(t time.Time) *time.Time { return new(t) }

func contains(values []string, v string) bool {
	return slices.Contains(values, v)
}
