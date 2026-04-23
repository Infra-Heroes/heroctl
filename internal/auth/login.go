package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceAuthResponse is the response from the device_authorization endpoint.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// StartDeviceFlow initiates the device authorization flow and returns
// the device auth response containing the user code and verification URI.
func StartDeviceFlow(ctx context.Context, authDomain, clientID string) (*DeviceAuthResponse, error) {
	endpoint := fmt.Sprintf("https://%s/oauth/v2/device_authorization", authDomain)
	body := url.Values{
		"client_id": {clientID},
		"scope":     {"openid profile email offline_access urn:zitadel:iam:user:resourceowner"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device_authorization request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device_authorization returned HTTP %d", resp.StatusCode)
	}

	var dar DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&dar); err != nil {
		return nil, fmt.Errorf("decode device_authorization response: %w", err)
	}
	if dar.DeviceCode == "" {
		return nil, fmt.Errorf("empty device_code in response — check your auth_domain and client_id")
	}
	return &dar, nil
}

// PollToken polls the token endpoint until the user completes
// authentication or the context is cancelled.
func PollToken(ctx context.Context, authDomain, clientID, deviceCode string, interval int) (*Token, error) {
	endpoint := fmt.Sprintf("https://%s/oauth/v2/token", authDomain)
	if interval <= 0 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("authentication timed out or cancelled")
		case <-ticker.C:
		}

		tr, err := postToken(ctx, endpoint, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {clientID},
		})
		if err != nil {
			return nil, err
		}

		switch tr.Error {
		case "":
			return &Token{
				AccessToken:  tr.AccessToken,
				RefreshToken: tr.RefreshToken,
				Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
				TokenType:    tr.TokenType,
			}, nil
		case "authorization_pending":
			// keep polling
		case "slow_down":
			ticker.Reset(time.Duration(interval+5) * time.Second)
		case "expired_token":
			return nil, fmt.Errorf("device code expired — run 'heroctl login' again")
		case "access_denied":
			return nil, fmt.Errorf("access denied")
		default:
			return nil, fmt.Errorf("token error %q: %s", tr.Error, tr.ErrorDesc)
		}
	}
}

// Refresh exchanges a refresh token for a new access token.
func Refresh(ctx context.Context, authDomain, clientID, refreshToken string) (*Token, error) {
	endpoint := fmt.Sprintf("https://%s/oauth/v2/token", authDomain)
	tr, err := postToken(ctx, endpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"scope":         {"openid profile email offline_access urn:zitadel:iam:user:resourceowner"},
	})
	if err != nil {
		return nil, err
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("refresh error %q: %s", tr.Error, tr.ErrorDesc)
	}
	return &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		TokenType:    tr.TokenType,
	}, nil
}

func postToken(ctx context.Context, endpoint string, vals url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &tr, nil
}
