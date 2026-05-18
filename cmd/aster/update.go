package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"aster/internal/selfupdate"
)

func updateCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update aster to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if Version == "dev" && !force {
				fmt.Println("Development build, self-update disabled. Use --force to override.")
				return nil
			}

			proxy := proxyFromEnv()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			fmt.Println("Checking for updates...")
			rel, err := selfupdate.FetchLatestRelease(ctx, &selfupdate.FetchOptions{Proxy: proxy})
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}

			if !force && !selfupdate.IsNewer(Version, rel.TagName) {
				fmt.Printf("Already up to date (current: %s, latest: %s).\n", Version, rel.TagName)
				return nil
			}

			fmt.Printf("Updating %s → %s ...\n", Version, rel.TagName)

			applyCtx, applyCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer applyCancel()
			if err := selfupdate.Apply(applyCtx, rel, proxy, func(phase string, pct int) {
				fmt.Printf("\r  [%3d%%] %s", pct, phase)
				if pct == 100 {
					fmt.Println()
				}
			}); err != nil {
				return fmt.Errorf("update failed: %w", err)
			}

			fmt.Printf("Successfully updated to %s. Please restart aster.\n", rel.TagName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force update even if already up to date or dev build")

	return cmd
}

func proxyFromEnv() string {
	for _, key := range []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
