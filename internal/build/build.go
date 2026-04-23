// Package build holds binary-level configuration values baked in at build time
// via -ldflags. The defaults are intentionally empty so that a misconfigured
// build fails loudly rather than silently connecting to the wrong server.
package build

var (
	// ServerURL is the hero-api base URL, e.g. "https://api.hero.example.com".
	ServerURL = ""
	// AuthDomain is the auth provider host, e.g. "auth.example.com".
	AuthDomain = ""
	// ClientID is the OAuth2 device-flow client ID.
	ClientID = ""
	// Version is the release tag injected at build time, e.g. "v1.2.3". Falls
	// back to "dev" for local builds without ldflags injection.
	Version = "dev"
)
