package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
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

// envKeyRe matches valid POSIX environment variable names.
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateEnv validates env keys and non-secret values from hero.toml.
// Values starting with "secret:" are skipped — their resolved values are
// validated server-side after Vault lookup.
// Keys must be valid POSIX names; values must not contain whitespace.
func validateEnv(env map[string]string) error {
	for k, v := range env {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("env key %q is invalid: must start with a letter or underscore and contain only letters, digits, and underscores", k)
		}
		if strings.HasPrefix(v, "secret:") {
			continue
		}
		for _, r := range v {
			if unicode.IsSpace(r) {
				return fmt.Errorf("env key %q: value must not contain whitespace (kernel cmdline transport limitation)", k)
			}
		}
	}
	return nil
}
