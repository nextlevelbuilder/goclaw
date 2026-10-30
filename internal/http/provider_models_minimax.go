package http

import "strings"

// currentMiniMaxTextModels affects discovery only. Explicit saved/custom IDs
// still pass through unchanged when an agent sends a request. Gateway IDs are
// retained exactly as returned by that gateway; no model routes are invented.
func currentMiniMaxTextModels(models []ModelInfo) []ModelInfo {
	result := make([]ModelInfo, 0, len(models))
	var slots []int
	var flash, m3 []ModelInfo
	for _, model := range models {
		id := strings.ToLower(model.ID)
		if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
			id = id[slash+1:]
		}
		isMSeries := strings.HasPrefix(id, "minimax-m") && len(id) > len("minimax-m") &&
			id[len("minimax-m")] >= '0' && id[len("minimax-m")] <= '9'
		isText := isMSeries || strings.HasPrefix(id, "minimax-text-") || id == "minimax-01"
		if !isText {
			result = append(result, model)
			continue
		}
		switch id {
		case "minimax-m3.1-flash", "minimax-m3.1-flash:free":
			flash = append(flash, model)
		case "minimax-m3":
			m3 = append(m3, model)
		default:
			continue
		}
		slots = append(slots, len(result))
		result = append(result, model)
	}
	// Reorder only the MiniMax slots, preserving other vendors and media.
	for i, model := range append(flash, m3...) {
		result[slots[i]] = model
	}
	return result
}
