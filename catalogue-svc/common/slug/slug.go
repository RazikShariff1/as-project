// Package slug builds URL slugs from display names.
package slug

import "strings"

// Make lowercases s and collapses every run of non-alphanumeric characters into a single hyphen.
// It returns "" when s has no letters or digits.
func Make(s string) string {
	var b strings.Builder

	pendingHyphen := false

	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}

			pendingHyphen = false

			b.WriteRune(r)

			continue
		}

		pendingHyphen = true
	}

	return b.String()
}

// Valid reports whether s is already a well-formed slug (lowercase letters, digits and single hyphens).
func Valid(s string) bool { return s != "" && Make(s) == s }

// Resolve returns the given slug, or one generated from name when none was given.
func Resolve(given, name string) string {
	if strings.TrimSpace(given) != "" {
		return strings.TrimSpace(given)
	}

	return Make(name)
}
