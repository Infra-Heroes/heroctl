// Package main is the entry point for heroctl.
package main

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Infra-Heroes/heroctl/internal/cmd"
)

func main() {
	setupLogger()
	cmd.Execute()
}

func setupLogger() {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var writer io.Writer = os.Stdout
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		if !strings.Contains(logFile, "..") {
			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //#nosec G304 G703 - path validated above
			if err == nil {
				writer = io.MultiWriter(os.Stdout, f)
			}
		}
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(writer, opts)))
}
