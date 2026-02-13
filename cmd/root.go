package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "snare",
	Short: "JiT catching test generator for Go",
	Long:  `snare generates ephemeral, mutation-based tests for Go code changes using Claude as the LLM backend.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
