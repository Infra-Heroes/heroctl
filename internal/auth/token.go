// Package auth handles Zitadel device code authentication and token storage.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Token holds the OAuth2 tokens persisted to disk.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	TokenType    string    `json:"token_type"`
}

// IsExpired reports whether the access token has expired (with a 30-second
// buffer to avoid using a token that expires mid-request).
func (t *Token) IsExpired() bool {
	return time.Now().After(t.Expiry.Add(-30 * time.Second))
}

func tokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "heroctl", "token.json"), nil
}

// Load reads the token file.  Returns an error if the file is missing or
// malformed — callers should direct the user to run 'heroctl login'.
func Load() (*Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) // #nosec G304 — path derived from user home
	if err != nil {
		return nil, fmt.Errorf("not authenticated — run 'heroctl login' first")
	}
	defer f.Close()

	var tok Token
	if err := json.NewDecoder(f).Decode(&tok); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token file is empty — run 'heroctl login' again")
	}
	return &tok, nil
}

// Save writes tok to the token file atomically, creating parent dirs if needed.
func Save(tok *Token) error {
	path, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600) // #nosec G304
	if err != nil {
		return fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(tok)
}
