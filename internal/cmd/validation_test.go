package cmd

import (
	"strings"
	"testing"
)

func TestValidateAppName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// valid
		{name: "single char", input: "a", wantErr: false},
		{name: "single digit", input: "9", wantErr: false},
		{name: "normal name", input: "my-app", wantErr: false},
		{name: "with hyphens", input: "hello-world-123", wantErr: false},
		{name: "all digits", input: "123", wantErr: false},
		{name: "two chars", input: "ab", wantErr: false},
		{name: "max length 63", input: strings.Repeat("a", 31) + "-" + strings.Repeat("b", 31), wantErr: false},

		// invalid: format
		{name: "empty string", input: "", wantErr: true},
		{name: "uppercase letters", input: "MyApp", wantErr: true},
		{name: "underscore", input: "my_app", wantErr: true},
		{name: "dot", input: "my.app", wantErr: true},
		{name: "slash", input: "my/app", wantErr: true},
		{name: "space", input: "my app", wantErr: true},

		// invalid: leading/trailing hyphen
		{name: "leading hyphen", input: "-myapp", wantErr: true},
		{name: "trailing hyphen", input: "myapp-", wantErr: true},
		{name: "only hyphens", input: "---", wantErr: true},

		// invalid: too long
		{name: "64 chars", input: strings.Repeat("a", 64), wantErr: true},
		{name: "100 chars", input: strings.Repeat("a", 100), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
			}
		})
	}
}
