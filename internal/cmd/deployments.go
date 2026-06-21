package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func deploymentsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "Manage deployments",
	}

	var listProject string
	list := &cobra.Command{
		Use:   "list",
		Short: "List deployments for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, listProject)
			if err != nil {
				return err
			}
			ds, err := deps.Client.ListDeployments(cmd.Context(), project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			if len(ds) == 0 {
				fmt.Println("No deployments for this project.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "APP\tSTATUS\tREPLICAS\tSCOPE\tHOSTNAME\tIMAGE\tCPU\tMEM(MB)\tPORT\tCREATED")
			for _, d := range ds {
				scope := d.ServiceScope
				if scope == "" {
					scope = "public"
				}
				replicasStr := fmt.Sprintf("%d", d.Replicas)
				if d.MinReplicas != d.MaxReplicas {
					replicasStr = fmt.Sprintf("%d/%d-%d", d.Replicas, d.MinReplicas, d.MaxReplicas)
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
					d.AppName, d.Status, replicasStr, scope, d.Hostname, d.Image, d.CPU, d.MemoryMB, d.Port, d.CreatedAt)
			}
			return w.Flush()
		},
	}
	list.Flags().StringVar(&listProject, "project", "", "Project name (required)")
	_ = list.MarkFlagRequired("project")

	var getProject string
	get := &cobra.Command{
		Use:   "get <app>",
		Short: "Get the latest deployment for an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, getProject)
			if err != nil {
				return err
			}
			d, err := deps.Client.GetDeployment(cmd.Context(), project.ID, args[0])
			if err != nil {
				return fmt.Errorf("get deployment: %w", err)
			}
			scope := d.ServiceScope
			if scope == "" {
				scope = "public"
			}
			fmt.Printf("App:       %s\n", d.AppName)
			fmt.Printf("Status:    %s\n", d.Status)
			fmt.Printf("Replicas:  %d (min: %d, max: %d)\n", d.Replicas, d.MinReplicas, d.MaxReplicas)
			fmt.Printf("Scope:     %s\n", scope)
			fmt.Printf("Hostname:  %s\n", d.Hostname)
			fmt.Printf("Image:     %s\n", d.Image)
			fmt.Printf("CPU:       %d\n", d.CPU)
			fmt.Printf("Memory MB: %d\n", d.MemoryMB)
			fmt.Printf("Port:      %d\n", d.Port)
			if len(d.Labels) > 0 {
				var labelPairs []string
				for k, v := range d.Labels {
					labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
				}
				fmt.Printf("Labels:    %s\n", strings.Join(labelPairs, ", "))
			}
			fmt.Printf("Created:   %s\n", d.CreatedAt)
			return nil
		},
	}
	get.Flags().StringVar(&getProject, "project", "", "Project name (required)")
	_ = get.MarkFlagRequired("project")

	var stopProject string
	stop := &cobra.Command{
		Use:   "stop <app>",
		Short: "Stop the active deployment for an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, stopProject)
			if err != nil {
				return err
			}
			if err := deps.Client.StopDeployment(cmd.Context(), project.ID, args[0]); err != nil {
				return fmt.Errorf("stop deployment: %w", err)
			}
			fmt.Printf("Deployment %q stopped.\n", args[0])
			return nil
		},
	}
	stop.Flags().StringVar(&stopProject, "project", "", "Project name (required)")
	_ = stop.MarkFlagRequired("project")

	var deleteProject string
	del := &cobra.Command{
		Use:   "delete <app>",
		Short: "Hard-delete a deployment (stops if running, removes all records)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, deleteProject)
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteDeployment(cmd.Context(), project.ID, args[0]); err != nil {
				return fmt.Errorf("delete deployment: %w", err)
			}
			fmt.Printf("Deployment %s deleted.\n", args[0])
			return nil
		},
	}
	del.Flags().StringVar(&deleteProject, "project", "", "Project name (required)")
	_ = del.MarkFlagRequired("project")

	var startProject string
	start := &cobra.Command{
		Use:   "start <app>",
		Short: "Start a stopped or failed deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, startProject)
			if err != nil {
				return err
			}
			d, err := deps.Client.StartDeployment(cmd.Context(), project.ID, args[0])
			if err != nil {
				return fmt.Errorf("start deployment: %w", err)
			}
			fmt.Printf("Deployment %q started. Status: %s\n", d.AppName, d.Status)
			if d.Hostname != "" {
				fmt.Printf("URL: https://%s\n", d.Hostname)
			}
			return nil
		},
	}
	start.Flags().StringVar(&startProject, "project", "", "Project name (required)")
	_ = start.MarkFlagRequired("project")

	var restartProject string
	restart := &cobra.Command{
		Use:   "restart <app>",
		Short: "Restart a running deployment in-place",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, restartProject)
			if err != nil {
				return err
			}
			d, err := deps.Client.RestartDeployment(cmd.Context(), project.ID, args[0])
			if err != nil {
				return fmt.Errorf("restart deployment: %w", err)
			}
			fmt.Printf("Deployment %q restarted. Status: %s\n", d.AppName, d.Status)
			if d.Hostname != "" {
				fmt.Printf("URL: https://%s\n", d.Hostname)
			}
			return nil
		},
	}
	restart.Flags().StringVar(&restartProject, "project", "", "Project name (required)")
	_ = restart.MarkFlagRequired("project")

	var sshProject string
	var sshCmd string
	ssh := &cobra.Command{
		Use:   "ssh <app>",
		Short: "Start an interactive shell session in the active deployment container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, sshProject)
			if err != nil {
				return err
			}

			fmt.Printf("Connecting to shell for app %q in project %q...\n", args[0], project.Name)
			conn, err := deps.Client.SSHDeployment(cmd.Context(), project.ID, args[0], sshCmd)
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			// Put local terminal in raw mode to forward input/signals properly
			fd := int(os.Stdin.Fd())
			if term.IsTerminal(fd) {
				oldState, err := term.MakeRaw(fd)
				if err != nil {
					return fmt.Errorf("set raw terminal: %w", err)
				}
				defer func() { _ = term.Restore(fd, oldState) }()
			}

			// Bidirectional copy
			errCh := make(chan error, 2)
			go func() {
				_, err := io.Copy(conn, os.Stdin)
				errCh <- err
			}()
			go func() {
				_, err := io.Copy(os.Stdout, conn)
				errCh <- err
			}()

			// Wait for either copy to finish or error
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case err := <-errCh:
				if err != nil && err != io.EOF {
					return err
				}
			}
			return nil
		},
	}
	ssh.Flags().StringVar(&sshProject, "project", "", "Project name (required)")
	_ = ssh.MarkFlagRequired("project")
	ssh.Flags().StringVar(&sshCmd, "cmd", "", "Command to execute (default /bin/sh)")

	cmd.AddCommand(list, get, stop, del, start, restart, ssh)
	return cmd
}
