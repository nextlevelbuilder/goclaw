package channels

// DeleteMessage removes one entry from the in-memory pending history and from
// the pending flush buffer. Persisted rows are left for the archive/compaction
// worker; undo only needs to stop the deleted message from reaching the next
// agent turn.
func (ph *PendingHistory) DeleteMessage(historyKey, messageID string) {
	if ph == nil || messageID == "" {
		return
	}
	ph.mu.Lock()
	entries := ph.entries[historyKey]
	if len(entries) > 0 {
		kept := entries[:0]
		for _, entry := range entries {
			if entry.MessageID != messageID {
				kept = append(kept, entry)
			}
		}
		ph.entries[historyKey] = kept
	}
	ph.mu.Unlock()

	ph.flushMu.Lock()
	if len(ph.flushBuf) > 0 {
		kept := ph.flushBuf[:0]
		for _, msg := range ph.flushBuf {
			if msg.PlatformMsgID != messageID {
				kept = append(kept, msg)
			}
		}
		ph.flushBuf = kept
	}
	ph.flushMu.Unlock()
}
