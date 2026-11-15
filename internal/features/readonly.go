package features

import "slices"

// CanRead grants no editing capability. Group/package/tenant scope is checked
// by control before projecting these business fields.
func CanRead(grants []string, definition Definition, field string) bool {
	if !slices.ContainsFunc(definition.Fields, func(f Field) bool { return f.Key == field }) {
		return false
	}
	return slices.Contains(grants, "groups:read") || CanEdit(grants, definition, field)
}
