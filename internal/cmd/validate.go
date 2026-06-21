package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/toml"
)

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [file]",
		Short: "Validate the syntax of a hero.toml file",
		Long:  `Validates the syntax and rules of a hero.toml deployment configuration file. Defaults to hero.toml in the current directory if no file is provided.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fileName := "hero.toml"
			if len(args) > 0 {
				fileName = args[0]
			}

			path, err := filepath.Abs(fileName)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()

			if _, err := toml.Parse(f); err != nil {
				return fmt.Errorf("validation failed for %s:\n%w", fileName, err)
			}

			fmt.Printf("✅ %s is valid.\n", fileName)
			return nil
		},
	}
}
