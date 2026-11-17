package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A secret can be set from a script: the value comes from the pipe, one trailing
// newline (what echo adds) is dropped, nothing else is touched.
func TestSecretsSet_ReadsTheValueFromAPipe(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/proj-1/secrets":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for name, tc := range map[string]struct{ in, want string }{
		"with newline":  {"s3cret value\n", "s3cret value"},
		"with crlf":     {"s3cret value\r\n", "s3cret value"},
		"without":       {"s3cret value", "s3cret value"},
		"inner newline": {"line1\nline2\n", "line1\nline2"},
	} {
		gotBody = nil
		cmd := secretsCmd(newTestDeps(srv))
		cmd.SetIn(strings.NewReader(tc.in))
		cmd.SetArgs([]string{"set", "TOKEN", "--project", "app"})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if gotBody["key"] != "TOKEN" || gotBody["value"] != tc.want {
			t.Errorf("%s: sent %v, want value %q", name, gotBody, tc.want)
		}
	}
}

func TestSecretsSet_AnEmptyPipeIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
			return
		}
		t.Errorf("an empty value must not reach the API: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	cmd := secretsCmd(newTestDeps(srv))
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{"set", "TOKEN", "--project", "app"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("err = %v", err)
	}
}
