package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aster/internal/selfupdate"
)

func updateCmd() *cobra.Command {
	var force bool
	var targetVersion string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update aster to the latest beta release",
		Long:  "Update aster to the latest beta (or stable) release.\n\nUse --version to force-install a specific tag, e.g. an alpha build:\n  aster update --version v1.1.0-alpha-2",
		RunE: func(cmd *cobra.Command, args []string) error {
			proxy := proxyFromEnv()

			if targetVersion != "" {
				return runVersionedUpdate(targetVersion, proxy)
			}

			if Version == "dev" && !force {
				fmt.Println("Development build, self-update disabled. Use --force to override.")
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			fmt.Println("Checking for updates...")
			releases, err := selfupdate.FetchReleases(ctx, &selfupdate.FetchOptions{Proxy: proxy})
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}

			rel := selfupdate.SelectLatest(releases, selfupdate.DefaultChannels)
			if rel == nil {
				fmt.Println("No beta or stable release available.")
				return nil
			}

			if !force && !selfupdate.IsNewer(Version, rel.TagName) {
				fmt.Printf("Already up to date (current: %s, latest: %s).\n", Version, rel.TagName)
				return nil
			}

			return applyUpdate(rel, proxy)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force update even if already up to date or dev build")
	cmd.Flags().StringVar(&targetVersion, "version", "", "Force-install a specific tag (e.g. v1.1.0-alpha-2); bypasses channel and newer checks")

	return cmd
}

func runVersionedUpdate(target, proxy string) error {
	tag := target
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Fetching release %s ...\n", tag)
	rel, err := selfupdate.FetchReleaseByTag(ctx, tag, &selfupdate.FetchOptions{Proxy: proxy})
	if err != nil {
		return fmt.Errorf("fetch release %s: %w", tag, err)
	}

	fmt.Printf("Forcing install of %s (current: %s) ...\n", rel.TagName, Version)
	return applyUpdate(rel, proxy)
}

func applyUpdate(rel *selfupdate.Release, proxy string) error {
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
