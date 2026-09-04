package migrator

import (
	"fmt"
	"strings"
)

// ParseRenameMappings converts OLD_NAME=NEW_NAME entries into the rename map
// used by the workflow and script generators.
func ParseRenameMappings(mappings []string) (map[string]string, error) {
	renameMap := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}
	return renameMap, nil
}

// ExcludeSecrets removes any secret names present in exclude from secrets,
// preserving order.
func ExcludeSecrets(secrets, exclude []string) []string {
	if len(exclude) == 0 {
		return secrets
	}
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		excludeSet[name] = struct{}{}
	}
	filtered := make([]string, 0, len(secrets))
	for _, name := range secrets {
		if _, excluded := excludeSet[name]; excluded {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}
