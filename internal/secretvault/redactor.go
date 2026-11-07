package secretvault

import (
	"sort"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Mask replaces every redacted value.
const Mask = "[REDACTED]"

// shortValueLen is the length below which a needle is only redacted when it
// stands alone (not inside a word). Replacing a 1-3 character value everywhere
// would corrupt ordinary output; the trade-off is that such a value glued to
// other word characters is not caught. Operators should use real secrets.
const shortValueLen = 4

// Redactor removes secret values (and their common encodings) from text. It is
// safe for concurrent use. An empty redactor is the identity.
type Redactor struct {
	mu       sync.RWMutex
	needles  map[string]struct{}
	replacer *strings.Replacer // needles >= shortValueLen, longest first
	short    []string          // needles < shortValueLen, boundary-matched
}

// NewRedactor returns an empty redactor.
func NewRedactor() *Redactor {
	return &Redactor{needles: map[string]struct{}{}}
}

// AddValue registers a secret value and its encodings.
func (r *Redactor) AddValue(value string) {
	r.add(Variants(value))
}

// AddPayload registers every sensitive part of a payload.
func (r *Redactor) AddPayload(p store.SecretVaultPayload) {
	r.add(PayloadVariants(p.Username, p.Secret))
}

func (r *Redactor) add(values []string) {
	if r == nil || len(values) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := r.needles[v]; ok {
			continue
		}
		r.needles[v] = struct{}{}
		changed = true
	}
	if changed {
		r.rebuild()
	}
}

// rebuild recomputes the matchers. Caller holds the write lock. It always
// builds fresh slices: Redact iterates the previous short slice outside the
// lock, so that backing array must never be written again.
func (r *Redactor) rebuild() {
	long := make([]string, 0, len(r.needles))
	short := make([]string, 0, len(r.short)+1)
	for n := range r.needles {
		if len(n) < shortValueLen {
			short = append(short, n)
		} else {
			long = append(long, n)
		}
	}
	sort.Slice(long, func(i, j int) bool {
		if len(long[i]) != len(long[j]) {
			return len(long[i]) > len(long[j])
		}
		return long[i] < long[j]
	})
	sort.Slice(short, func(i, j int) bool { return len(short[i]) > len(short[j]) })
	r.short = short
	if len(long) == 0 {
		r.replacer = nil
		return
	}
	pairs := make([]string, 0, len(long)*2)
	for _, n := range long {
		pairs = append(pairs, n, Mask)
	}
	r.replacer = strings.NewReplacer(pairs...)
}

// Empty reports whether nothing is registered.
func (r *Redactor) Empty() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.needles) == 0
}

// Redact replaces every registered value in s. It is idempotent.
func (r *Redactor) Redact(s string) string {
	if r == nil || s == "" {
		return s
	}
	r.mu.RLock()
	replacer, short := r.replacer, r.short
	r.mu.RUnlock()
	if replacer != nil {
		s = replacer.Replace(s)
	}
	for _, n := range short {
		s = replaceBounded(s, n)
	}
	return s
}

func replaceBounded(s, needle string) string {
	if !strings.Contains(s, needle) {
		return s
	}
	var b strings.Builder
	last := 0
	for i := 0; i+len(needle) <= len(s); {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			break
		}
		start := i + j
		end := start + len(needle)
		if hasBoundaryAt(s, start, end) {
			b.WriteString(s[last:start])
			b.WriteString(Mask)
			last = end
			i = end
			continue
		}
		i = start + 1
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// Func returns the redactor as a plain function (assignable to
// tracing.TextRedactor). A nil redactor yields nil so callers can skip it.
func (r *Redactor) Func() func(string) string {
	if r == nil {
		return nil
	}
	return r.Redact
}
