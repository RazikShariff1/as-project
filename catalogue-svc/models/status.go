package models

// Statuses shared by brands, categories, products and variants, as in catalogue-service.
const (
	StatusEnabled = "ENABLED"
	StatusHidden  = "HIDDEN"
)

// ValidStatus reports whether s is ENABLED or HIDDEN.
func ValidStatus(s string) bool { return s == StatusEnabled || s == StatusHidden }
