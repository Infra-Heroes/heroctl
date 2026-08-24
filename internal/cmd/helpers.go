package cmd

import (
	"context"
	"fmt"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// resolveProject looks up a project by name and returns it.
// Returns a descriptive error if not found.
func resolveProject(ctx context.Context, deps *Deps, projectName string) (*client.Project, error) {
	projects, err := deps.Client.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	for i := range projects {
		if projects[i].Name == projectName {
			return &projects[i], nil
		}
	}
	return nil, fmt.Errorf("project %q not found; create it first with: heroctl projects create %s",
		projectName, projectName)
}
