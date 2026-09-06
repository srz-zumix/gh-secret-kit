package migrator

import (
	"fmt"
	"strings"
)

// ValidateSecretName reports whether name is a valid GitHub secret name. The
// generated scripts interpolate secret names into shell commands (grep patterns
// and gh secret set arguments), so names outside this pattern are rejected
// before they can reach a script. secretNamePattern is defined alongside the
// codespaces script generator in the same package.
func ValidateSecretName(name string) error {
	if !secretNamePattern.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: expected letters, digits and underscores, not starting with a digit", name)
	}
	return nil
}

// validateSecretNames rejects any source or renamed secret name that cannot be
// safely interpolated into a generated script. It is called at every workflow
// generator boundary so a name can never reach the shell unchecked, regardless
// of how the caller built the rename map.
func validateSecretNames(secrets []string, rename map[string]string) error {
	for _, name := range secrets {
		if err := ValidateSecretName(name); err != nil {
			return err
		}
		if renamed, ok := rename[name]; ok {
			if err := ValidateSecretName(renamed); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParseRenameMappings converts OLD_NAME=NEW_NAME entries into the rename map
// used by the workflow and script generators. Both sides are validated as
// secret names so unsafe values are rejected before they reach a generator.
func ParseRenameMappings(mappings []string) (map[string]string, error) {
	renameMap := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		if err := ValidateSecretName(parts[0]); err != nil {
			return nil, fmt.Errorf("invalid rename source name in %q: %w", mapping, err)
		}
		if err := ValidateSecretName(parts[1]); err != nil {
			return nil, fmt.Errorf("invalid rename destination name in %q: %w", mapping, err)
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
