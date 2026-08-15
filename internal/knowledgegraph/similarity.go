package knowledgegraph

import "strings"

// JaroWinkler computes the Jaro-Winkler similarity between two strings (0.0–1.0).
// Higher values indicate greater similarity. Case-insensitive comparison.
func JaroWinkler(a, b string) float64 {
	a = strings.ToLower(a)
	b = strings.ToLower(b)

	if a == b {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Jaro similarity
	matchDist := max(max(len(a), len(b))/2-1, 0)

	aMatched := make([]bool, len(a))
	bMatched := make([]bool, len(b))

	var matches, transpositions float64
	for i := range a {
		lo := max(0, i-matchDist)
		hi := min(len(b), i+matchDist+1)
		for j := lo; j < hi; j++ {
			if bMatched[j] || a[i] != b[j] {
				continue
			}
			aMatched[i] = true
			bMatched[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0.0
	}

	k := 0
	for i := range a {
		if !aMatched[i] {
			continue
		}
		for !bMatched[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}

	jaro := (matches/float64(len(a)) + matches/float64(len(b)) + (matches-transpositions/2)/matches) / 3.0

	// Winkler prefix bonus (up to 4 chars, scaling factor 0.1)
	prefix := 0
	for i := 0; i < min(4, min(len(a), len(b))); i++ {
		if a[i] == b[i] {
			prefix++
		} else {
			break
		}
	}

	return jaro + float64(prefix)*0.1*(1-jaro)
}
