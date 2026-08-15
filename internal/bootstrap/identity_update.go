package bootstrap

import "strings"

// UpdateIdentityField replaces a single field line in IDENTITY.md content without
// touching any other fields. Handles both plain format ("Name: value") and the
// LLM-generated markdown format ("- **Name:** value") written during onboarding.
// If no matching line is found, one is inserted after the first heading.
func UpdateIdentityField(content, fieldName, newValue string) string {
	if newValue == "" {
		return content
	}
	marker := fieldName + ":"
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Strip leading list/bold markdown markers (-, *, #, spaces) to normalize
		// both "Name: value" and "- **Name:** value" to the same comparison base.
		stripped := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*# \t"))
		if strings.HasPrefix(stripped, marker) {
			// Find the marker colon in the original line by locating the marker text,
			// NOT LastIndex — values may contain colons (e.g. Avatar URLs).
			markerIdx := strings.Index(stripped, ":")
			// Map back to original line: find where stripped starts in the original.
			prefixLen := strings.Index(line, stripped[:markerIdx+1])
			colonIdx := prefixLen + markerIdx
			// Include any "**" immediately after the colon so ":**" stays intact.
			afterColon := line[colonIdx+1:]
			starLen := len(afterColon) - len(strings.TrimLeft(afterColon, "*"))
			lines[i] = line[:colonIdx+1+starLen] + " " + newValue
			return strings.Join(lines, "\n")
		}
	}
	// No existing line for this field — insert after first heading or at top.
	insertAt := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			insertAt = i + 1
			break
		}
	}
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, fieldName+": "+newValue)
	result = append(result, lines[insertAt:]...)
	return strings.Join(result, "\n")
}
