package cmd

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

const sandboxKind = "sandbox"

// sandboxPromise is the file promise of contract §1, shown in the help.
const sandboxPromise = "Everything on your sandbox's filesystem survives stop, start, restarts and host moves until you `reset` or `delete` the sandbox. Running processes do not survive a stop or a host failure. A hardware failure can lose only the last seconds of unflushed writes. If your balance stays exhausted for 30 days, the sandbox and its data are deleted; the debt remains."

// sandboxLoginNote replaces execHelpNote: exec is always enabled on a sandbox.
const sandboxLoginNote = `It needs an interactive login: run "heroctl login". A personal access token (--token) is
refused.`

// consoleArgv is what "sandbox console" runs on the PTY: one persistent tmux session, so
// reconnecting returns to it.
var consoleArgv = []string{"tmux", "new", "-A", "-s", "main"}

// stdinIsTerminal is replaced in tests.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) } // #nosec G115 -- a file descriptor always fits in an int

func sandboxCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Persistent Linux VMs to work in",
		Long: `A sandbox is a persistent Linux VM from the official base image, billed like any
deployment. Open it with "console" or run one command with "exec".

` + sandboxPromise + `

During the trial the storage behind a sandbox has a single replica.

"sandbox console" attaches to a tmux session ("tmux new -A -s main"), so laptop sleep,
network loss and session limits do not end your work: reconnecting returns to it.

` + sandboxLoginNote,
		Example: `  heroctl sandbox create dev --project shop
  heroctl sandbox console dev --project shop
  heroctl sandbox exec dev --project shop -- git --version`,
	}
	cmd.AddCommand(
		sandboxCreateCmd(deps), sandboxListCmd(deps),
		sandboxLifecycleCmd(deps, "start"), sandboxLifecycleCmd(deps, "stop"),
		sandboxDeleteCmd(deps), sandboxResetCmd(deps),
		sandboxConsoleCmd(deps), sandboxExecCmd(deps),
	)
	return cmd
}

func projectFlag(c *cobra.Command, dst *string) {
	c.Flags().StringVar(dst, "project", "", "Project name (required)")
	_ = c.MarkFlagRequired("project")
}

func sandboxCreateCmd(deps *Deps) *cobra.Command {
	var project string
	var cpu, memory, disk int
	var secrets []string
	c := &cobra.Command{
		Use:   "create <name> --project <project> [--cpu N] [--memory MB] [--disk GB] [--secret KEY ...]",
		Short: "Create a sandbox",
		Long: `Create a sandbox. Without flags the server picks its defaults (1 vCPU, 2048 MB memory,
20 GB disk). Each --secret KEY puts the project secret KEY into the sandbox's environment.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateAppName(name); err != nil {
				return err
			}
			var env map[string]string
			if len(secrets) > 0 {
				env = make(map[string]string, len(secrets))
				for _, k := range secrets {
					env[k] = "secret:" + k
				}
				if err := validateEnv(env); err != nil {
					return err
				}
			}
			p, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			if _, err := deps.Client.CreateSandbox(cmd.Context(), p.ID, client.CreateSandboxRequest{
				AppName: name, CPU: cpu, MemoryMB: memory, DiskGB: disk, Env: env,
			}); err != nil {
				return deploymentError(err, project)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sandbox %q created. Open it with: heroctl sandbox console %s --project %s\n", name, name, project)
			return nil
		},
	}
	projectFlag(c, &project)
	c.Flags().IntVar(&cpu, "cpu", 0, "vCPUs (server default if unset)")
	c.Flags().IntVar(&memory, "memory", 0, "Memory in MB (server default if unset)")
	c.Flags().IntVar(&disk, "disk", 0, "Disk size in GB (server default if unset)")
	c.Flags().StringArrayVar(&secrets, "secret", nil, "Project secret to put in the environment, by key (repeatable)")
	return c
}

func sandboxListCmd(deps *Deps) *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "list --project <project>",
		Short: "List the sandboxes of a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			ds, err := deps.Client.ListDeployments(cmd.Context(), p.ID)
			if err != nil {
				return fmt.Errorf("list sandboxes: %w", err)
			}
			out := cmd.OutOrStdout()
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			n := 0
			for _, d := range ds {
				if d.Kind != sandboxKind {
					continue
				}
				if n == 0 {
					_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tCPU\tMEM(MB)\tCREATED")
				}
				n++
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", d.AppName, d.Status, d.CPU, d.MemoryMB, d.CreatedAt)
			}
			if n == 0 {
				_, _ = fmt.Fprintln(out, "No sandboxes in this project.")
				return nil
			}
			return w.Flush()
		},
	}
	projectFlag(c, &project)
	return c
}

// sandboxLifecycleCmd builds "start" or "stop".
func sandboxLifecycleCmd(deps *Deps, verb string) *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   verb + " <name> --project <project>",
		Short: map[string]string{"start": "Start a stopped sandbox", "stop": "Stop a sandbox (files are kept, running processes end)"}[verb],
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			if verb == "start" {
				_, err = deps.Client.StartDeployment(cmd.Context(), p.ID, args[0])
			} else {
				err = deps.Client.StopDeployment(cmd.Context(), p.ID, args[0])
			}
			if err != nil {
				return fmt.Errorf("%s sandbox: %w", verb, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sandbox %q: %s requested.\n", args[0], verb)
			return nil
		},
	}
	projectFlag(c, &project)
	return c
}

// confirmDestructive asks for confirmation unless yes is set. It needs a terminal to ask
// on and refuses without one. It reports whether to go ahead.
func confirmDestructive(cmd *cobra.Command, yes bool, verb, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if !stdinIsTerminal() {
		return false, fmt.Errorf("%s needs confirmation and there is no terminal to ask on; run again with --yes", verb)
	}
	if !confirm(cmd.InOrStdin(), prompt) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
		return false, nil
	}
	return true, nil
}

func sandboxDeleteCmd(deps *Deps) *cobra.Command {
	var project string
	var yes bool
	c := &cobra.Command{
		Use:   "delete <name> --project <project> [--yes]",
		Short: "Delete a sandbox and everything on its disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := confirmDestructive(cmd, yes, "delete", fmt.Sprintf("Delete sandbox %q and everything on its disk? This cannot be undone.", args[0]))
			if err != nil || !ok {
				return err
			}
			p, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteDeployment(cmd.Context(), p.ID, args[0]); err != nil {
				return fmt.Errorf("delete sandbox: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sandbox %q deleted.\n", args[0])
			return nil
		},
	}
	projectFlag(c, &project)
	c.Flags().BoolVar(&yes, "yes", false, "Do not ask for confirmation")
	return c
}

func sandboxResetCmd(deps *Deps) *cobra.Command {
	var project string
	var yes bool
	c := &cobra.Command{
		Use:   "reset <name> --project <project> [--yes]",
		Short: "Replace a stopped sandbox's disk with a fresh copy of the current base image",
		Long: `Delete the sandbox's disk and clone it again from the current base image. Everything
you changed on the filesystem is lost. Only a stopped sandbox can be reset: stop it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := confirmDestructive(cmd, yes, "reset", fmt.Sprintf("Reset sandbox %q? Everything on its disk is replaced by a fresh base image.", args[0]))
			if err != nil || !ok {
				return err
			}
			p, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			if err := deps.Client.ResetDeployment(cmd.Context(), p.ID, args[0]); err != nil {
				return fmt.Errorf("reset sandbox: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sandbox %q reset. Start it with: heroctl sandbox start %s --project %s\n", args[0], args[0], project)
			return nil
		},
	}
	projectFlag(c, &project)
	c.Flags().BoolVar(&yes, "yes", false, "Do not ask for confirmation")
	return c
}

func sandboxConsoleCmd(deps *Deps) *cobra.Command {
	var project string
	var raw bool
	c := &cobra.Command{
		Use:   "console <name> --project <project> [--raw]",
		Short: "Open your tmux session in a sandbox",
		Long: `Open an interactive session in a running sandbox. It attaches to the tmux session
"main" (created on first use), so reconnecting returns to your work. --raw opens a plain
login shell instead, without tmux.

` + sandboxLoginNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var argv []string // nil is a plain shell
			if !raw {
				argv = consoleArgv
			}
			return openConsole(cmd, deps, project, args[0], "", argv)
		},
	}
	projectFlag(c, &project)
	c.Flags().BoolVar(&raw, "raw", false, "Plain shell, no tmux")
	return c
}

func sandboxExecCmd(deps *Deps) *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "exec <name> --project <project> -- <command> [args...]",
		Short: "Run one command in a running sandbox",
		Long: `Run one command in a running sandbox. stdin, stdout and stderr are passed through
unchanged and heroctl exits with the command's exit code.

` + sandboxLoginNote,
		Example: "  heroctl sandbox exec dev --project shop -- ls -l /home/hero",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 || len(args) < 2 {
				return errors.New("usage: heroctl sandbox exec <name> --project <project> -- <command> [args...]")
			}
			return runRemote(cmd, deps, project, args[0], "", args[1:])
		},
	}
	projectFlag(c, &project)
	return c
}
