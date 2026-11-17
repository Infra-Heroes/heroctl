package cmd

import (
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func TestInvitationNextSteps(t *testing.T) {
	sent := invitationNextSteps(&client.InvitationCreated{Email: "a@example.org", Token: "tok", EmailSent: true})
	if !strings.Contains(sent, "was mailed to a@example.org") || strings.Contains(sent, "No mail was sent") {
		t.Errorf("mailed case: %q", sent)
	}
	by := invitationNextSteps(&client.InvitationCreated{Email: "a@example.org", Token: "tok"})
	if !strings.Contains(by, "No mail was sent") || !strings.Contains(by, "heroctl accept-invite tok") {
		t.Errorf("by-hand case: %q", by)
	}
}
