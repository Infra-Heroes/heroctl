// Package exec opens SSH sessions to app VMs through the exec gateway: it issues the
// credentials, tunnels SSH over a WebSocket and runs commands or a console on top.
package exec

import "strings"

// Quote joins argv into the command string hero-guest accepts (contract §4): every word
// single-quoted, a single quote inside a word written as '\” (close the quote, escaped
// quote, reopen), words separated by one space. hero-guest splits it again and runs
// execve without a shell.
func Quote(argv []string) string {
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(words, " ")
}
