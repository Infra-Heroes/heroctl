package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func orgsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Show org info and credit balance",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			org, err := deps.Client.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("get org: %w", err)
			}

			credits, err := deps.Client.GetCredits(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("get credits: %w", err)
			}

			fmt.Printf("Name:     %s\n", org.Name)
			fmt.Printf("VM cap:   %d\n", org.VmCap)
			fmt.Printf("Credits:  %s\n", formatCredits(credits))
			return nil
		},
	}

	cmd.AddCommand(orgsSetLimitsCmd(deps))
	return cmd
}

func orgsSetLimitsCmd(deps *Deps) *cobra.Command {
	var (
		orgID       string
		maxProjects int32
		vmCap       int32
		maxCPU      int32
		maxMemoryMB int32
	)

	cmd := &cobra.Command{
		Use:   "set-limits",
		Short: "Update org limits (platform admins only)",
		Long: `Update per-org limits. Requires owner/admin membership in the
InfraHeroes instance org. Omitted flags are left unchanged.

Without --org the caller's own org is targeted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if orgID == "" {
				org, err := deps.Client.GetOrg(ctx)
				if err != nil {
					return fmt.Errorf("get org: %w", err)
				}
				orgID = org.ID
			}

			var mp, vc, mc, mm *int32
			if cmd.Flags().Changed("max-projects") {
				mp = &maxProjects
			}
			if cmd.Flags().Changed("vm-cap") {
				vc = &vmCap
			}
			if cmd.Flags().Changed("max-cpu") {
				mc = &maxCPU
			}
			if cmd.Flags().Changed("max-memory-mb") {
				mm = &maxMemoryMB
			}
			if mp == nil && vc == nil && mc == nil && mm == nil {
				return fmt.Errorf("at least one of --max-projects, --vm-cap, --max-cpu, --max-memory-mb is required")
			}

			limits, err := deps.Client.UpdateOrgLimits(ctx, orgID, mp, vc, mc, mm)
			if err != nil {
				return fmt.Errorf("update org limits: %w", err)
			}

			fmt.Printf("Org:            %s (%s)\n", limits.Name, limits.ID)
			fmt.Printf("Max projects:   %d\n", limits.MaxProjects)
			fmt.Printf("VM cap:         %d\n", limits.VmCap)
			fmt.Printf("Max CPU:        %d\n", limits.MaxCpu)
			fmt.Printf("Max memory MB:  %d\n", limits.MaxMemoryMb)
			return nil
		},
	}

	cmd.Flags().StringVar(&orgID, "org", "", "Target org ID (defaults to the caller's org)")
	cmd.Flags().Int32Var(&maxProjects, "max-projects", 0, "Maximum number of projects")
	cmd.Flags().Int32Var(&vmCap, "vm-cap", 0, "Maximum number of running VMs")
	cmd.Flags().Int32Var(&maxCPU, "max-cpu", 0, "Maximum CPU cores per deployment")
	cmd.Flags().Int32Var(&maxMemoryMB, "max-memory-mb", 0, "Maximum memory (MB) per deployment")

	return cmd
}
