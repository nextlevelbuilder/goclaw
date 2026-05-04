// Obsidian wikilink extraction for memory markdown files.
//
// Wikilink syntax (Obsidian flavor):
//
//	[[Note Name]]                 — basic link
//	[[Note Name|Display Text]]    — link with display alias
//	[[Folder/Note]]               — link by relative folder path
//	[[Note#Heading]]              — link to a section
//	[[Note#^block-id]]            — link to a block
//	![[Note]]                     — embedded link (transclusion)
//
// Extracted Link records preserve the raw target string and a parsed
// link kind (LinkKindWiki / LinkKindEmbed / LinkKindBlock) so downstream
// callers can render or follow them differently.
//
// Resolution to an actual on-vault path lives in ResolveWikilink — given
// the link target ("Folder/Note", "Note", or "Note#section"), it
// searches a path-set and returns the best match. Obsidian convention:
// case-insensitive basename match wins; ties broken by lexicographic
// folder order. Unresolved targets return ("", false) — the caller
// should still record the raw target so backlinks can be re-resolved
// later when the missing page is added to the vault.
package memory

import (
	"path/filepath"
	"regexp"
	"strings"
)

// LinkKind enumerates the Obsidian wikilink variants we recognize.
type LinkKind string

const (
	LinkKindWiki  LinkKind = "wiki"  // [[Note]] or [[Note|Display]]
	LinkKindEmbed LinkKind = "embed" // ![[Note]]
	LinkKindBlock LinkKind = "block" // [[Note#^block-id]]
)

// Link is a parsed wikilink reference, before path resolution.
type Link struct {
	Kind    LinkKind // wiki | embed | block
	Target  string   // raw target as written between [[ and ]] (excludes #section/#^block, excludes |display)
	Section string   // optional #heading (without leading #)
	BlockID string   // optional ^block-id (without leading ^)
	Display string   // optional |alias (empty if absent)
}

// wikilinkRE matches [[...]], capturing the inner content. Also matches
// the leading ! for embed variants (handled by checking the byte before
// the match in the caller).
var wikilinkRE = regexp.MustCompile(`!?\[\[([^\[\]]+)\]\]`)

// ExtractLinks scans a markdown body for Obsidian wikilinks and returns
// them in document order. Duplicates are NOT deduplicated — callers that
// want unique targets should dedup themselves (the seeder does, since
// memory_links uses a unique constraint on (from_path, to_basename, link_type)).
//
// Code blocks (```...``` and indented blocks) are NOT skipped; in
// practice Obsidian users rarely embed wikilinks in code, and the cost
// of full markdown parsing isn't justified for this read path. If false
// positives appear in the wild, the caller can post-filter.
func ExtractLinks(body string) []Link {
	matches := wikilinkRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]Link, 0, len(matches))
	for _, m := range matches {
		// m = [start, end, group1Start, group1End]
		full := body[m[0]:m[1]]
		inner := body[m[2]:m[3]]
		isEmbed := strings.HasPrefix(full, "!")
		out = append(out, parseLinkInner(inner, isEmbed))
	}
	return out
}

// parseLinkInner splits an inner wikilink string ("Folder/Note#Heading|Display")
// into its components.
func parseLinkInner(s string, isEmbed bool) Link {
	link := Link{Kind: LinkKindWiki}
	if isEmbed {
		link.Kind = LinkKindEmbed
	}
	// Split off |display first so a # inside the display alias isn't
	// mistaken for a section anchor (Obsidian doesn't really allow this
	// but be defensive).
	if pipe := strings.Index(s, "|"); pipe >= 0 {
		link.Display = strings.TrimSpace(s[pipe+1:])
		s = s[:pipe]
	}
	// Then split off #section / #^block.
	if hash := strings.Index(s, "#"); hash >= 0 {
		anchor := s[hash+1:]
		s = s[:hash]
		if strings.HasPrefix(anchor, "^") {
			link.Kind = LinkKindBlock
			link.BlockID = strings.TrimSpace(anchor[1:])
		} else {
			link.Section = strings.TrimSpace(anchor)
		}
	}
	link.Target = strings.TrimSpace(s)
	return link
}

// ResolveWikilink picks the best vault path for a given link Target,
// using Obsidian's resolution rules:
//
//  1. If Target contains a "/", treat as a path hint — match docs whose
//     vault-relative path ends with "<Target>.md" or "<Target>" (case-
//     insensitive).
//  2. Else (basename-only target): match docs whose basename (without
//     ".md") equals Target case-insensitively. If multiple match,
//     prefer shorter relative paths (Obsidian leans toward the closest
//     in folder hierarchy; lex order is a stable tiebreaker).
//
// candidates is the full set of vault-relative .md paths the resolver
// can choose from. Returns the resolved path and true on success;
// ("", false) if no candidate matches.
func ResolveWikilink(target string, candidates []string) (string, bool) {
	if target == "" || len(candidates) == 0 {
		return "", false
	}
	tLower := strings.ToLower(target)

	if strings.ContainsRune(target, '/') {
		// Path-hint resolution. Try with and without ".md".
		withMd := tLower
		if !strings.HasSuffix(withMd, ".md") {
			withMd += ".md"
		}
		var best string
		for _, c := range candidates {
			cl := strings.ToLower(c)
			if cl == withMd || strings.HasSuffix(cl, "/"+withMd) {
				if best == "" || len(c) < len(best) || (len(c) == len(best) && c < best) {
					best = c
				}
			}
		}
		if best != "" {
			return best, true
		}
		return "", false
	}

	// Basename-only resolution.
	var best string
	for _, c := range candidates {
		base := filepath.Base(c)
		base = strings.TrimSuffix(base, ".md")
		if strings.EqualFold(base, target) {
			if best == "" || len(c) < len(best) || (len(c) == len(best) && c < best) {
				best = c
			}
		}
	}
	if best != "" {
		return best, true
	}
	_ = tLower
	return "", false
}
