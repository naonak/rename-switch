package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Watch monitors cfg.GamesDir for new game files and processes them as they appear.
// It runs an initial scan of all existing files, then polls every interval.
// If doCleanup is true, Cleanup is run after each batch of new files.
// Blocks until SIGINT or SIGTERM.
func Watch(cfg *Config, interval time.Duration, doCleanup bool) {
	seen := map[string]bool{}

	// Initial scan
	initialFiles := collectGameFiles(cfg.GamesDir, cfg.Recursive)
	processWatchBatch(cfg, initialFiles, seen, doCleanup)

	colorPrintf(colorCyan, "\nWatching %s (interval: %s) — Ctrl+C to stop\n",
		cfg.GamesDir, interval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			files := collectGameFiles(cfg.GamesDir, cfg.Recursive)
			var newFiles []string
			for _, f := range files {
				if !seen[f] {
					newFiles = append(newFiles, f)
				}
			}
			if len(newFiles) > 0 {
				colorPrintf(colorCyan, "\n[WATCH] %d nouveau(x) fichier(s) détecté(s)\n",
					len(newFiles))
				processWatchBatch(cfg, newFiles, seen, doCleanup)
			}
		case <-sigCh:
			colorPrint(colorCyan, "\nWatch stopped.\n")
			return
		}
	}
}

// processWatchBatch processes a list of files, marks them as seen, and
// optionally runs cleanup afterwards.
func processWatchBatch(cfg *Config, files []string, seen map[string]bool, doCleanup bool) {
	errorsLog := cfg.GamesDir + "/_errors.log"

	count, errors := 0, 0
	for _, f := range files {
		seen[f] = true
		if err := ProcessFile(cfg, f); err != nil {
			errors++
			if cfg.Apply {
				appendLine(errorsLog, f)
			}
		} else {
			count++
		}
	}

	if len(files) > 0 {
		colorPrintf(colorCyan, "Processed: %d files", count)
		if errors > 0 {
			colorPrintf(colorRed, ", %d error(s)", errors)
		}
		colorPrint(colorCyan, "\n")
	}

	if doCleanup && len(files) > 0 {
		cleanupDir := cfg.DestDir
		if cleanupDir == "" {
			cleanupDir = cfg.GamesDir
		}
		Cleanup(cleanupDir, cfg.NstoolPath, cfg.Apply)
	}
}
