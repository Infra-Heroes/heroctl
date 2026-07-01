package auth

import (
	"testing"
	"time"
)

func TestTokenIsExpired(t *testing.T) {
	tests := []struct {
		name   string
		expiry time.Time
		want   bool
	}{
		{name: "far in the future", expiry: time.Now().Add(1 * time.Hour), want: false},
		{name: "already past", expiry: time.Now().Add(-1 * time.Hour), want: true},
		{name: "within the 30s buffer", expiry: time.Now().Add(10 * time.Second), want: true},
		{name: "just outside the 30s buffer", expiry: time.Now().Add(45 * time.Second), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := &Token{Expiry: tt.expiry}
			if got := tok.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() with expiry %v = %v, want %v", tt.expiry, got, tt.want)
			}
		})
	}
}
