package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// findCmd walks the command tree following the given path of command names.
func findCmd(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("command %q not found under %q", name, cur.Name())
		}
		cur = next
	}
	return cur
}

func TestNeedsAuth(t *testing.T) {
	root := newRootCmd()

	// Cobra only attaches the generated help/completion commands once the
	// tree is initialised, which Execute does. Trigger it explicitly.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	exempt := [][]string{
		{"login"},
		{"signup"},
		{"validate"},
		{"version"},
		{"help"},
		{"completion"},
		// Regression: Name() returns the leaf ("bash"), so matching on the
		// leaf name alone made every shell variant demand a token.
		{"completion", "bash"},
		{"completion", "zsh"},
		{"completion", "fish"},
		{"completion", "powershell"},
	}
	for _, path := range exempt {
		t.Run("exempt/"+strings.Join(path, " "), func(t *testing.T) {
			if got := needsAuth(findCmd(t, root, path...)); got {
				t.Errorf("needsAuth(%q) = true, want false", strings.Join(path, " "))
			}
		})
	}

	guarded := [][]string{
		{"deploy"},
		{"logs"},
		{"orgs"},
		{"orgs", "set-limits"},
		{"projects"},
		{"projects", "list"},
		{"projects", "delete"},
		{"secrets", "set"},
		{"volumes", "destroy"},
		{"tokens", "create"},
		{"deployments", "delete"},
		{"members", "list"},
	}
	for _, path := range guarded {
		t.Run("guarded/"+strings.Join(path, " "), func(t *testing.T) {
			if got := needsAuth(findCmd(t, root, path...)); !got {
				t.Errorf("needsAuth(%q) = false, want true", strings.Join(path, " "))
			}
		})
	}
}

// The hidden commands a shell runs on every TAB press must never require a
// token, or completion breaks in any shell whose session has an expired one.
func TestNeedsAuthHiddenCompletionCommands(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		c := &cobra.Command{Use: name}
		root.AddCommand(c)
		if needsAuth(c) {
			t.Errorf("needsAuth(%q) = true, want false", name)
		}
	}
}
