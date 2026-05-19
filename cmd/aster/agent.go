package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"aster/internal/tui"
)

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent profiles",
	}

	var force bool
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset all agent profiles to built-in defaults (overwrites local changes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			names := tui.DefaultAgentNames()
			fmt.Println("This will overwrite the following agent profiles:")
			for _, n := range names {
				fmt.Printf("  - %s\n", n)
			}

			if !force {
				fmt.Print("\nAll local changes will be lost. Continue? [y/N] ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			reset, err := tui.ResetAgentDefaults()
			if err != nil {
				return fmt.Errorf("reset agents: %w", err)
			}
			for _, name := range reset {
				fmt.Printf("  ✓ %s\n", name)
			}
			fmt.Printf("Reset %d agent profile(s) to defaults.\n", len(reset))
			return nil
		},
	}
	resetCmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	cmd.AddCommand(resetCmd)
	return cmd
}
