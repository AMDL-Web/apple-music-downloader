package media

import "strings"

// firstNonEmpty returns the first value that is not blank, unchanged, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
