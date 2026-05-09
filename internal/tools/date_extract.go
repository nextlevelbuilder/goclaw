package tools

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateRange represents an extracted date range from a query.
type DateRange struct {
	From time.Time
	To   time.Time
}

var (
	// yearMonths maps month names (English + Indonesian) to month numbers.
	yearMonths = map[string]time.Month{
		// English
		"january": time.January, "february": time.February, "march": time.March,
		"april": time.April, "may": time.May, "june": time.June,
		"july": time.July, "august": time.August, "september": time.September,
		"october": time.October, "november": time.November, "december": time.December,
		// Indonesian
		"januari": time.January, "februari": time.February, "maret": time.March,
		"mei": time.May, "juni": time.June, "juli": time.July,
		"agustus": time.August, "oktober": time.October, "desember": time.December,
		// Abbreviations
		"jan": time.January, "feb": time.February, "mar": time.March,
		"apr": time.April, "jun": time.June, "jul": time.July,
		"aug": time.August, "sep": time.September, "oct": time.October,
		"nov": time.November, "dec": time.December,
	}

	// datePatterns ordered by specificity (most specific first).
	datePatterns = []*regexp.Regexp{
		// "8th May 2026", "08 Mei 2026", "7th May", "7 Mei"
		regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+(` + monthAlternation() + `)(?:\s+(\d{4}))?\b`),
		// "May 8th 2026", "Mei 08 2026", "May 8"
		regexp.MustCompile(`(?i)\b(` + monthAlternation() + `)\s+(\d{1,2})(?:st|nd|rd|th)?(?:\s+(\d{4}))?\b`),
		// "2026-05-08", "2026/05/08"
		regexp.MustCompile(`\b(\d{4})[-/](\d{1,2})[-/](\d{1,2})\b`),
		// "08-05-2026", "08/05/2026"
		regexp.MustCompile(`\b(\d{1,2})[-/](\d{1,2})[-/](\d{4})\b`),
	}

	// fullDatePattern matches any of the date patterns for stripping.
	fullDatePattern = regexp.MustCompile(
		`(?i)(?:\b\d{1,2}(?:st|nd|rd|th)?\s+(?:` + monthAlternation() + `)(?:\s+\d{4})?\b` +
			`|\b(?:` + monthAlternation() + `)\s+\d{1,2}(?:st|nd|rd|th)?(?:\s+\d{4})?\b` +
			`|\b\d{4}[-/]\d{1,2}[-/]\d{1,2}\b` +
			`|\b\d{1,2}[-/]\d{1,2}[-/]\d{4}\b)`,
	)
)

func monthAlternation() string {
	var names []string
	for name := range yearMonths {
		names = append(names, name)
	}
	return strings.Join(names, "|")
}

// ExtractDateRange attempts to find a date reference in the query.
// Returns nil if no date found. Uses current year as default.
func ExtractDateRange(query string) *DateRange {
	for _, pat := range datePatterns {
		groups := pat.FindStringSubmatch(query)
		if groups == nil {
			continue
		}

		day, month, year, ok := parseDateGroups(pat, groups)
		if !ok {
			continue
		}

		if year == 0 {
			year = time.Now().Year()
		}

		from := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		// End of day + 1 day buffer for timezone safety
		to := from.AddDate(0, 0, 2)
		return &DateRange{From: from, To: to}
	}
	return nil
}

func parseDateGroups(pat *regexp.Regexp, groups []string) (day int, month time.Month, year int, ok bool) {
	switch {
	// Pattern 1: "8th May 2026" → groups[1]=day, groups[2]=month, groups[3]=year
	case len(groups) == 4 && isDayFirstPattern(groups[1], groups[2]):
		day, ok = atoi(groups[1])
		if !ok {
			return
		}
		month, ok = lookupMonth(groups[2])
		if !ok {
			return
		}
		if groups[3] != "" {
			year, _ = atoi(groups[3])
		}
		return day, month, year, day >= 1 && day <= 31

	// Pattern 2: "May 8th 2026" → groups[1]=month, groups[2]=day, groups[3]=year
	case len(groups) == 4 && isMonthFirst(groups[1]):
		month, ok = lookupMonth(groups[1])
		if !ok {
			return
		}
		day, ok = atoi(groups[2])
		if !ok {
			return
		}
		if groups[3] != "" {
			year, _ = atoi(groups[3])
		}
		return day, month, year, day >= 1 && day <= 31

	// Pattern 3: "2026-05-08" → groups[1]=year, groups[2]=month, groups[3]=day
	case len(groups) == 4 && isYearFirst(groups[1]):
		year, ok = atoi(groups[1])
		if !ok {
			return
		}
		var m int
		m, ok = atoi(groups[2])
		if !ok || m < 1 || m > 12 {
			return 0, 0, 0, false
		}
		day, ok = atoi(groups[3])
		if !ok {
			return
		}
		return day, time.Month(m), year, day >= 1 && day <= 31

	// Pattern 4: "08-05-2026" → groups[1]=day, groups[2]=month, groups[3]=year
	case len(groups) == 4:
		day, ok = atoi(groups[1])
		if !ok {
			return
		}
		var m int
		m, ok = atoi(groups[2])
		if !ok || m < 1 || m > 12 {
			return 0, 0, 0, false
		}
		year, ok = atoi(groups[3])
		if !ok {
			return
		}
		return day, time.Month(m), year, day >= 1 && day <= 31
	}
	return 0, 0, 0, false
}

func isDayFirstPattern(g1, g2 string) bool {
	_, err1 := strconv.Atoi(g1)
	_, ok2 := lookupMonth(g2)
	return err1 == nil && ok2
}

func isMonthFirst(g1 string) bool {
	_, ok := lookupMonth(g1)
	return ok
}

func isYearFirst(g1 string) bool {
	n, err := strconv.Atoi(g1)
	return err == nil && n >= 1900 && n <= 2100
}

func lookupMonth(name string) (time.Month, bool) {
	m, ok := yearMonths[strings.ToLower(name)]
	return m, ok
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// StripDateTokens removes date references from the query for FTS/vector search.
func StripDateTokens(query string) string {
	return strings.TrimSpace(fullDatePattern.ReplaceAllString(query, ""))
}
