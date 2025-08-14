package discovery

import (
	"strings"
)

const (
	// Well-known seed for the Silmaril discovery catalog
	WellKnownSeed = "silmaril-discovery-v1"
	
	// Maximum size for BEP 44 value (1000 bytes)
	MaxValueSize = 1000
)

// ModelCatalog is the catalog of all discoverable models
type ModelCatalog struct {
	Version  int                    `json:"v"`
	Sequence int64                  `json:"seq"`
	Updated  int64                  `json:"t"`
	Models   map[string]ModelEntry  `json:"m"`
}

// ModelEntry is a single model in the catalog
type ModelEntry struct {
	InfoHash string   `json:"h"`
	Size     int64    `json:"s,omitempty"`
	Tags     []string `json:"t,omitempty"`
	Added    int64    `json:"a"`
}

// extractTags extracts searchable tags from a model name
func extractTags(name string) []string {
	var tags []string
	lower := strings.ToLower(name)
	
	// Extract org/model parts
	if parts := strings.Split(lower, "/"); len(parts) > 0 {
		tags = append(tags, parts[0])
		if len(parts) > 1 {
			for _, part := range strings.Split(parts[1], "-") {
				if len(part) > 2 {
					tags = append(tags, part)
				}
			}
		}
	}
	
	// Common sizes
	for _, size := range []string{"3b", "7b", "8b", "13b", "70b"} {
		if strings.Contains(lower, size) {
			tags = append(tags, size)
		}
	}
	
	// Model families
	for _, family := range []string{"llama", "mistral", "gpt", "gemma", "phi"} {
		if strings.Contains(lower, family) {
			tags = append(tags, family)
		}
	}
	
	return tags
}

// matchesPattern checks if a name matches a search pattern
func matchesPattern(name, pattern string) bool {
	// Handle wildcard pattern
	if pattern == "*" || pattern == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}