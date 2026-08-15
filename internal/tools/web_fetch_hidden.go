package tools

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// hiddenClasses contains CSS class names used by popular frameworks to hide elements.
// These are well-known, stable class names unlikely to appear in non-hiding contexts.
// Only base "always hidden" classes — no responsive breakpoint variants (hidden-xs, d-md-none).
var hiddenClasses = map[string]bool{
	// Tailwind CSS (v3, v4)
	"hidden":    true, // display: none
	"invisible": true, // visibility: hidden
	"collapse":  true, // visibility: collapse
	"sr-only":   true, // screen-reader only (clip + fixed position)
	// Bootstrap (v3, v4, v5)
	"d-none":          true, // display: none
	"visually-hidden": true, // clip-based sr-only (Bootstrap 5)
	// NOT hidden (becomes visible on focus): visually-hidden-focusable, sr-only-focusable
	// Bulma
	"is-hidden":    true, // display: none
	"is-invisible": true, // visibility: hidden
	"is-sr-only":   true, // screen-reader only
	// Foundation (Zurb)
	"hide":        true, // display: none
	"show-for-sr": true, // screen-reader only
	// UIKit
	"uk-hidden":    true, // display: none
	"uk-invisible": true, // visibility: hidden
	// Materialize CSS
	// "hide" already listed under Foundation
	// Spectre.css
	"d-hide":         true, // display: none (alias of d-none)
	"d-invisible":    true, // visibility: hidden
	"text-hide":      true, // text-indent off-screen
	"text-assistive": true, // clip-based sr-only
	// Tachyons CSS
	"clip":       true, // clip + fixed position
	"dn":         true, // display: none
	"vis-hidden": true, // visibility: hidden
	// WordPress
	"screen-reader-text": true, // clip-based sr-only
	// Angular Material / CDK
	"cdk-visually-hidden": true, // clip-based sr-only
	// General conventions
	"offscreen": true, // position off-screen
	"clip-hide": true, // clip-based hiding
}

// reOffScreen matches negative positions commonly used to push elements off-screen.
// Matches -5000 and above (5+ digit negatives, or 4-digit starting with 5-9).
// Lower values like -1000 can be legitimate CSS (animations, slide menus).
var reOffScreen = regexp.MustCompile(`-[5-9]\d{3,}|-\d{5,}`)

// reZeroFontSize matches font-size:0 but not font-size:0.5em etc.
var reZeroFontSize = regexp.MustCompile(`(?i)font-size\s*:\s*0(?:\s*[;"]|$)`)

// reZeroOpacity matches opacity:0 but not opacity:0.5 etc.
var reZeroOpacity = regexp.MustCompile(`(?i)opacity\s*:\s*0(?:\s*[;"]|$)`)

// hasHiddenClass checks if any CSS class on the element matches known hidden class names.
func hasHiddenClass(n *html.Node) bool {
	classAttr := getAttr(n, "class")
	if classAttr == "" {
		return false
	}
	for cls := range strings.FieldsSeq(classAttr) {
		if hiddenClasses[strings.ToLower(cls)] {
			return true
		}
	}
	return false
}

// isHiddenElement detects elements hidden via HTML attributes, CSS classes, or inline CSS.
// Skips these elements and all descendants to prevent hidden-text injection attacks.
func isHiddenElement(n *html.Node) bool {
	// HTML5 hidden attribute
	for _, a := range n.Attr {
		if a.Key == "hidden" {
			return true
		}
	}
	// aria-hidden="true"
	if getAttr(n, "aria-hidden") == "true" {
		return true
	}
	// Known hidden CSS classes from popular frameworks
	if hasHiddenClass(n) {
		return true
	}
	// Inline style checks
	style := strings.ToLower(getAttr(n, "style"))
	if style == "" {
		return false
	}
	if strings.Contains(style, "display") && strings.Contains(style, "none") {
		return true
	}
	if strings.Contains(style, "visibility") && strings.Contains(style, "hidden") {
		return true
	}
	// Off-screen positioning
	if reOffScreen.MatchString(style) {
		return true
	}
	// Zero font-size (regex avoids matching 0.5em, 0.8rem, etc.)
	if reZeroFontSize.MatchString(style) {
		return true
	}
	// Zero opacity (regex avoids matching 0.5, 0.8, etc.)
	if reZeroOpacity.MatchString(style) {
		return true
	}
	return false
}
