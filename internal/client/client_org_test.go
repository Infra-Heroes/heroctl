package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
)

func orgTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	tok := &auth.Token{AccessToken: "t", Expiry: time.Now().Add(time.Hour)}
	return New(srv.URL, "auth.test", "client", tok)
}

var mine = []map[string]any{
	{"name": "dominik", "role": "owner", "zitadel_id": "392730715990524162", "current": false},
	{"name": "infraheroes", "role": "owner", "zitadel_id": "367725856946192642", "current": true},
}

func TestResolveOrg_ByNameOrIDAndNeverBeyondTheMemberships(t *testing.T) {
	c := orgTestClient(t, func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(map[string]any{"orgs": mine}) })
	for in, want := range map[string]string{
		"dominik":             "392730715990524162",
		"Dominik":             "392730715990524162",
		" infraheroes ":       "367725856946192642",
		"367725856946192642":  "367725856946192642",
		"392730715990524162 ": "392730715990524162",
	} {
		got, err := c.ResolveOrg(context.Background(), in)
		if err != nil || got != want {
			t.Errorf("%q -> %q (%v), want %q", in, got, err, want)
		}
	}
	_, err := c.ResolveOrg(context.Background(), "somebody-else")
	if err == nil || !strings.Contains(err.Error(), "dominik, infraheroes") {
		t.Errorf("a stranger's organisation must be refused and the yours listed: %v", err)
	}
	if _, err := c.ResolveOrg(context.Background(), "999999999"); err == nil {
		t.Error("an id that is not yours must be refused")
	}
}

func TestSetOrg_PutsTheHeaderOnEveryRequestAndNothingWithoutIt(t *testing.T) {
	var got []string
	c := orgTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get(OrgHeader))
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.SetOrg("392730715990524162")
	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "" || got[1] != "392730715990524162" {
		t.Errorf("headers = %q", got)
	}
}
