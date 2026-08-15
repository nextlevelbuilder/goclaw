package browser

// Role sets ported from OpenClaw TS pw-role-snapshot.ts:26-78.
// Used to determine which AX tree nodes get ref assignments.

// interactiveRoles are elements users can interact with.
// These always get a ref in snapshots.
var interactiveRoles = map[string]bool{
	"button":           true,
	"link":             true,
	"textbox":          true,
	"checkbox":         true,
	"radio":            true,
	"combobox":         true,
	"listbox":          true,
	"menuitem":         true,
	"menuitemcheckbox": true,
	"menuitemradio":    true,
	"option":           true,
	"searchbox":        true,
	"slider":           true,
	"spinbutton":       true,
	"switch":           true,
	"tab":              true,
	"treeitem":         true,
}

// contentRoles are meaningful content elements.
// These get a ref only when they have a name.
var contentRoles = map[string]bool{
	"heading":      true,
	"cell":         true,
	"gridcell":     true,
	"columnheader": true,
	"rowheader":    true,
	"listitem":     true,
	"article":      true,
	"region":       true,
	"main":         true,
	"navigation":   true,
}

// structuralRoles are layout/grouping elements.
// These never get refs. In compact mode, unnamed ones are removed.
var structuralRoles = map[string]bool{
	"generic":      true,
	"group":        true,
	"list":         true,
	"table":        true,
	"row":          true,
	"rowgroup":     true,
	"grid":         true,
	"treegrid":     true,
	"menu":         true,
	"menubar":      true,
	"toolbar":      true,
	"tablist":      true,
	"tree":         true,
	"directory":    true,
	"document":     true,
	"application":  true,
	"presentation": true,
	"none":         true,
}

// IsInteractive returns true if the role represents an interactive element.
func IsInteractive(role string) bool {
	return interactiveRoles[role]
}

// IsContent returns true if the role represents a content element.
func IsContent(role string) bool {
	return contentRoles[role]
}

// IsStructural returns true if the role represents a structural element.
func IsStructural(role string) bool {
	return structuralRoles[role]
}
