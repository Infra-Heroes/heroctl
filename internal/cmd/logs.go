package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/spf13/cobra"
)

func logsCmd(deps *Deps) *cobra.Command {
	var project, grep string
	var follow bool
	var tail int

	cmd := &cobra.Command{
		Use:   "logs <app>",
		Short: "Stream logs for a running deployment",
		Long: `Prints the logs of a running deployment, and with --follow keeps printing as
new lines arrive.

  heroctl logs my-app --project p                       what the app has logged
  heroctl logs my-app --project p --tail 50              only the last 50 lines
  heroctl logs my-app --project p --grep "error|panic"   only lines that match (a regular expression)
  heroctl logs my-app --project p -f --grep timeout      follow, showing matching lines only

The platform keeps the recent output of the running instance, not a history:
what a stopped or replaced instance logged is gone.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			re, err := compileGrep(grep)
			if err != nil {
				return err
			}
			if tail < 0 {
				return fmt.Errorf("--tail cannot be negative")
			}
			if tail > 0 && follow {
				return fmt.Errorf("--tail shows the end of what has been logged and cannot be combined with --follow")
			}
			proj, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			body, err := deps.Client.StreamLogs(cmd.Context(), proj.ID, args[0], follow)
			if err != nil {
				return fmt.Errorf("logs: %w", err)
			}
			defer func() { _ = body.Close() }()

			if tail > 0 || re != nil {
				return filterLogs(body, cmd.OutOrStdout(), tail, re)
			}

			// Copy raw bytes rather than scanning lines. bufio.Scanner caps a
			// token at 64KB and aborts the whole stream with ErrTooLong on a
			// longer line, so a single large log entry (a JSON dump, a stack
			// trace) would suppress all output, including the lines after it.
			if _, err := io.Copy(cmd.OutOrStdout(), body); err != nil {
				return fmt.Errorf("stream logs: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVar(&tail, "tail", 0, "Show only the last N lines (not with --follow)")
	cmd.Flags().StringVar(&grep, "grep", "", "Show only lines that match this regular expression")
	return cmd
}

func compileGrep(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("--grep %q is not a valid regular expression: %w", pattern, err)
	}
	return re, nil
}

// filterLogs copies the lines of r that match re (all of them when re is nil)
// to w; with tail above zero only the last tail of those are kept and written at
// the end. Lines are read without a length limit: a bufio.Scanner would stop the
// whole stream at its first line longer than 64KB.
func filterLogs(r io.Reader, w io.Writer, tail int, re *regexp.Regexp) error {
	br := bufio.NewReader(r)
	var kept []string
	for {
		line, err := br.ReadString('\n')
		if line != "" && (re == nil || re.MatchString(line)) {
			if tail > 0 {
				kept = append(kept, line)
				if len(kept) > tail {
					kept = kept[1:]
				}
			} else if _, werr := io.WriteString(w, line); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("stream logs: %w", err)
		}
	}
	for _, l := range kept {
		if _, err := io.WriteString(w, l); err != nil {
			return err
		}
	}
	return nil
}
