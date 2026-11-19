package cmd

import (
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func TestTeamTexts(t *testing.T) {
	team := client.Team{
		Members:  []client.TeamMember{{Email: "a@example.test"}, {Email: "b@example.test"}},
		Projects: []client.TeamProject{{ProjectName: "shop", Role: "editor"}},
	}
	if got := teamMembersText(team); got != "a@example.test, b@example.test" {
		t.Errorf("members = %q", got)
	}
	if got := teamProjectsText(team); got != "shop (editor)" {
		t.Errorf("projects = %q", got)
	}
	if teamMembersText(client.Team{}) != "-" || teamProjectsText(client.Team{}) != "-" {
		t.Error("an empty team reads as -")
	}
}

func TestExpiryText(t *testing.T) {
	at := "2027-02-17T07:00:00Z"
	if got := expiryText(&at); got != "2027-02-17" {
		t.Errorf("date = %q", got)
	}
	if expiryText(nil) != "never" {
		t.Error("no date reads as never")
	}
	empty := ""
	if expiryText(&empty) != "never" {
		t.Error("an empty date reads as never")
	}
}
