package cmd

import (
	"bufio"
	"fmt"
	"net/http"
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
	var yes bool

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
			_ = f.Close()
			if err != nil {
				return err
			}
			if err := validateAppName(heroCfg.App.Name); err != nil {
				return fmt.Errorf("hero.toml: %w", err)
			}
			if err := validateEnv(heroCfg.Env); err != nil {
				return fmt.Errorf("hero.toml [env]: %w", err)
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

			// 9. Resolve volume names → IDs.
			var volumeAttachments []client.VolumeAttachment
			if len(heroCfg.Volumes) > 0 {
				allVolumes, err := deps.Client.ListVolumes(ctx, project.ID)
				if err != nil {
					return fmt.Errorf("list volumes: %w", err)
				}
				volumeIndex := make(map[string]client.Volume, len(allVolumes))
				for _, v := range allVolumes {
					volumeIndex[v.Name] = v
				}
				for _, vc := range heroCfg.Volumes {
					v, ok := volumeIndex[vc.Name]
					if !ok {
						return fmt.Errorf("volume %q not found — create it first with: heroctl volumes create %s --size <gb> --project %s",
							vc.Name, vc.Name, projectName)
					}
					if v.Status == "attached" {
						fmt.Printf("Warning: volume %q is currently attached to another deployment; the API will enforce this.\n", vc.Name)
					}
					volumeAttachments = append(volumeAttachments, client.VolumeAttachment{
						VolumeID:  v.ID,
						MountPath: vc.Mount,
					})
				}
			}

			// 10. Confirm before deploying with volumes — the server must stop the
			// running allocation first, causing brief downtime.
			if len(volumeAttachments) > 0 && !yes {
				fmt.Println("Warning: this app has volumes attached.")
				fmt.Println("If an active deployment exists it will be stopped before the new one starts, causing brief downtime.")
				fmt.Print("Continue? [y/N] ")
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				if answer := strings.TrimSpace(strings.ToLower(scanner.Text())); answer != "y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// 11. Create deployment.
			fmt.Printf("Deploying to project %q (ID: %s)...\n", project.Name, project.ID)
			deployment, err := deps.Client.CreateDeployment(ctx, project.ID, client.CreateDeploymentRequest{
				AppName:     heroCfg.App.Name,
				Image:       image,
				CPU:         heroCfg.Deploy.CPU,
				MemoryMB:    heroCfg.Deploy.MemoryMB,
				Port:        heroCfg.Deploy.Port,
				Env:         heroCfg.Env,
				HealthPath:  heroCfg.Deploy.HealthPath,
				ScaleToZero: heroCfg.Deploy.ScaleToZero,
				Volumes:     volumeAttachments,
			})
			if err != nil {
				if strings.Contains(err.Error(), "vm cap") {
					return fmt.Errorf("%s — stop or delete an existing deployment first with: heroctl deployments list --project %s", err, projectName)
				}
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
					if deployment.Hostname == "" {
						return nil
					}
					healthURL := "https://" + deployment.Hostname + heroCfg.Deploy.HealthPath
					fmt.Printf("Waiting for app to become reachable at %s", healthURL)
					httpClient := &http.Client{Timeout: 5 * time.Second}
					for time.Now().Before(deadline) {
						resp, err := httpClient.Get(healthURL) //nolint:noctx
						if err == nil {
							_ = resp.Body.Close()
							if resp.StatusCode < 500 {
								fmt.Println(" ready!")
								fmt.Printf("URL: https://%s\n", deployment.Hostname)
								return nil
							}
						}
						fmt.Print(".")
						time.Sleep(3 * time.Second)
					}
					fmt.Println()
					return fmt.Errorf("timed out waiting for app to become reachable")
				case "dead":
					fmt.Println()
					return fmt.Errorf("deployment failed — app crashed on startup; check logs with: heroctl deployments logs %s --project %s", heroCfg.App.Name, projectName)
				default:
					fmt.Print(".")
				}
			}
			fmt.Println()
			return fmt.Errorf("timed out after %s waiting for deployment to start", deployTimeout)
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name to deploy to (required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt (for CI/non-interactive use)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}
