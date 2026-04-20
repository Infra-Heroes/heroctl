package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/build"
	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/toml"
)

const deployTimeout = 5 * time.Minute

func deployCmd(deps *Deps) *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build, push, and deploy the current directory via hero.toml",
		Long: `deploy reads hero.toml from the current directory, builds and pushes a
Docker image to the hero registry, then submits a new deployment to hero-api.

The project must already exist (create with: heroctl projects create <name>).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// 1. Parse hero.toml.
			f, err := os.Open("hero.toml")
			if err != nil {
				return fmt.Errorf("hero.toml not found in current directory: %w", err)
			}
			heroCfg, err := toml.Parse(f)
			f.Close()
			if err != nil {
				return err
			}
			if err := validateAppName(heroCfg.App.Name); err != nil {
				return fmt.Errorf("hero.toml: %w", err)
			}

			// 2. Get org (needed for image namespace).
			org, err := deps.Client.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("get org: %w", err)
			}

			// 3. Find project by name.
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			// 4. Get registry credentials.
			creds, err := deps.Client.RegistryCredentials(ctx)
			if err != nil {
				return fmt.Errorf("get registry credentials: %w", err)
			}

			// 5. docker login via stdin to avoid credentials appearing in process list.
			// hero-api proxies /v2/ — push to the API host, not the backend registry.
			registry := strings.TrimPrefix(strings.TrimPrefix(build.ServerURL, "https://"), "http://")
			fmt.Printf("Logging into registry %s...\n", registry)
			loginCmd := exec.CommandContext(ctx, "docker", "login", registry,
				"--username", creds.Username, "--password-stdin")
			loginCmd.Stdin = strings.NewReader(creds.Password)
			loginCmd.Stdout = os.Stdout
			loginCmd.Stderr = os.Stderr
			if err := loginCmd.Run(); err != nil {
				return fmt.Errorf("docker login: %w", err)
			}

			// 6. Determine the image tag: short git SHA if available, otherwise timestamp.
			var imageTag string
			if gitOut, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output(); err == nil {
				imageTag = strings.TrimSpace(string(gitOut))
			} else {
				imageTag = fmt.Sprintf("%d", time.Now().Unix())
			}

			// 7. Build image: {registry}/{orgName}/{projectName}:{imageTag}
			image := fmt.Sprintf("%s/%s/%s:%s", registry, org.Name, project.Name, imageTag)
			fmt.Printf("Building %s...\n", image)
			buildCmd := exec.CommandContext(ctx, "docker", "build", "--platform", "linux/amd64", "-t", image, ".")
			buildCmd.Stdout = os.Stdout
			buildCmd.Stderr = os.Stderr
			if err := buildCmd.Run(); err != nil {
				return fmt.Errorf("docker build: %w", err)
			}

			// 8. Push image.
			fmt.Printf("Pushing %s...\n", image)
			pushCmd := exec.CommandContext(ctx, "docker", "push", image)
			pushCmd.Stdout = os.Stdout
			pushCmd.Stderr = os.Stderr
			if err := pushCmd.Run(); err != nil {
				return fmt.Errorf("docker push: %w", err)
			}

			// 9. Create deployment.
			fmt.Printf("Deploying to project %q (ID: %s)...\n", project.Name, project.ID)
			deployment, err := deps.Client.CreateDeployment(ctx, project.ID, client.CreateDeploymentRequest{
				AppName:    heroCfg.App.Name,
				Image:      image,
				CPU:        heroCfg.Deploy.CPU,
				MemoryMB:   heroCfg.Deploy.MemoryMB,
				Port:       heroCfg.Deploy.Port,
				Env:        heroCfg.Env,
				HealthPath: heroCfg.Deploy.HealthPath,
			})
			if err != nil {
				return fmt.Errorf("create deployment: %w", err)
			}

			fmt.Printf("Deployment %q submitted. Waiting for VM to start", heroCfg.App.Name)

			deadline := time.Now().Add(deployTimeout)
			for time.Now().Before(deadline) {
				time.Sleep(5 * time.Second)
				nomadStatus, err := deps.Client.GetDeploymentStatus(ctx, project.ID, heroCfg.App.Name)
				if err != nil {
					// Transient error — keep trying.
					fmt.Print(".")
					continue
				}
				switch nomadStatus {
				case "running":
					fmt.Println(" running!")
					if deployment.Hostname != "" {
						fmt.Printf("URL: https://%s\n", deployment.Hostname)
					}
					return nil
				case "dead":
					fmt.Println()
					return fmt.Errorf("deployment failed — Nomad job is dead; check logs with: heroctl deployments get %s --project %s", heroCfg.App.Name, projectName)
				default:
					fmt.Print(".")
				}
			}
			fmt.Println()
			return fmt.Errorf("timed out after %s waiting for deployment to start", deployTimeout)
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name to deploy to (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}
