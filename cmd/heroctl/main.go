// Package main is the entry point for heroctl.
package main

import (
	"github.com/Infra-Heroes/NanoStackUtilities/logger"
	"github.com/Infra-Heroes/heroctl/internal/cmd"
)

func main() {
	logger.GetLogger() // initialise structured logger
	cmd.Execute()
}
