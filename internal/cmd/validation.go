package cmd

import (
	"fmt"
	"regexp"
)

// appNameRe matches valid app names: 1–63 characters, lowercase letters/digits/hyphens,
// must start and end with a letter or digit.
var appNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,61}[a-z0-9]$|^[a-z0-9]$`)

// validateAppName returns an error if name is not a valid app name.
func validateAppName(name string) error {
	if !appNameRe.MatchString(name) {
		return fmt.Errorf("app name %q is invalid: must be 1–63 characters, lowercase letters/digits/hyphens only, and cannot start or end with a hyphen", name)
	}
	return nil
}
