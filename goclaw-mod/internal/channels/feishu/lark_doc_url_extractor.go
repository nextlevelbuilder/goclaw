package feishu

import "regexp"

// DocRef points at a Lark docx document discovered in an inbound message. The
// channel uses it to fetch raw content via GetDocRawContent and inline the
// result into the agent's prompt so the LLM can reason over linked docs
// without a dedicated tool call.
type DocRef struct {
	DocID       string
	OriginalURL string
}

// larkDocxURLPattern matches Lark/Feishu docx document URLs. Scope is strictly
// `/docx/<id>` on lark domains — sheets, base, wiki, mindnote are out of scope
// for the initial implementation.
//
// The hostname class is restricted to real hostname characters
// ([a-zA-Z0-9._-]+) on purpose: a lazy `[^\s<>"']*?` would happily skip over
// query strings and match a second `.larksuite.com/docx/` embedded later in
// the URL (e.g. `https://foo.com?u=https://attacker.larksuite.com/docx/BAD`),
// causing the extractor to pluck a doc ID the user never saw. The stricter
// class prevents that by refusing `?` `&` `/` and similar URL separators
// before the `/docx/` segment.
var larkDocxURLPattern = regexp.MustCompile(
	`https?://[a-zA-Z0-9._-]+\.(?:larksuite\.(?:com|cn)|feishu\.cn)(?::\d+)?/docx/([A-Za-z0-9]+)`,
)

// extractLarkDocURLs returns each unique Lark docx document referenced in
// text, in the order they appear. Duplicates (same DocID) are collapsed so the
// caller only fetches each doc once per message.
func extractLarkDocURLs(text string) []DocRef {
	if text == "" {
		return nil
	}
	matches := larkDocxURLPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var refs []DocRef
	for _, m := range matches {
		// m[0]:m[1] is the full URL, m[2]:m[3] is the doc ID capture group.
		fullURL := text[m[0]:m[1]]
		docID := text[m[2]:m[3]]
		if _, dup := seen[docID]; dup {
			continue
		}
		seen[docID] = struct{}{}
		refs = append(refs, DocRef{DocID: docID, OriginalURL: fullURL})
	}
	return refs
}
