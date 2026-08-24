package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func volumesCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volumes",
		Short: "Manage volumes (Ceph RBD block devices) for a project",
	}

	cmd.AddCommand(
		volumesCreateCmd(deps),
		volumesListCmd(deps),
		volumesDestroyCmd(deps),
	)

	return cmd
}

func volumesCreateCmd(deps *Deps) *cobra.Command {
	var (
		projectName string
		sizeGB      int
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new volume for a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			vol, err := deps.Client.CreateVolume(ctx, project.ID, client.CreateVolumeRequest{
				Name:   name,
				SizeGB: sizeGB,
			})
			if err != nil {
				return fmt.Errorf("create volume: %w", err)
			}

			fmt.Printf("Volume %q created (%d GB), ID: %s\n", vol.Name, vol.SizeGB, vol.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().IntVar(&sizeGB, "size", 0, "Volume size in gigabytes (required)")
	_ = cmd.MarkFlagRequired("size")

	return cmd
}

func volumesListCmd(deps *Deps) *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all volumes for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			vols, err := deps.Client.ListVolumes(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list volumes: %w", err)
			}

			if len(vols) == 0 {
				fmt.Printf("No volumes. Create one with: heroctl volumes create <name> --size <gb> --project %s\n", projectName)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSIZE\tSTATUS\tATTACHED TO\tCREATED")
			for _, v := range vols {
				attachedTo := "-"
				if v.AttachedDeploymentID != nil {
					attachedTo = *v.AttachedDeploymentID
				}
				_, _ = fmt.Fprintf(w, "%s\t%d GB\t%s\t%s\t%s\n",
					v.Name, v.SizeGB, v.Status, attachedTo, v.CreatedAt)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func volumesDestroyCmd(deps *Deps) *cobra.Command {
	var (
		projectName string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy a volume (permanently deletes the block device)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			vols, err := deps.Client.ListVolumes(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list volumes: %w", err)
			}

			var target *client.Volume
			for i := range vols {
				if vols[i].Name == name {
					target = &vols[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("volume %q not found in project %q", name, projectName)
			}

			if target.Status == "attached" {
				return fmt.Errorf("volume %q is attached to a deployment; stop the deployment first", name)
			}

			if !force {
				fmt.Printf("Are you sure you want to permanently destroy volume %q (%d GB)? [y/N] ", name, target.SizeGB)
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			if err := deps.Client.DeleteVolume(ctx, project.ID, target.ID); err != nil {
				return fmt.Errorf("destroy volume: %w", err)
			}

			fmt.Printf("Volume %q destroyed.\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}
