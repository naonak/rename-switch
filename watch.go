package main

import (
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

var gameExts = map[string]bool{
	".nsp": true, ".xci": true, ".nsz": true, ".xcz": true,
}

// Watch monitors cfg.GamesDir for new game files and processes them as they appear.
// It uses fsnotify for instant detection and falls back to periodic polling on
// network mounts where inotify may be unavailable.
// Blocks until SIGINT or SIGTERM.
func Watch(cfg *Config, fallbackInterval time.Duration, doCleanup bool) {
	seen := map[string]bool{}

	// Initial scan
	initialFiles := collectGameFiles(cfg.GamesDir, cfg.Recursive)
	processWatchBatch(cfg, initialFiles, seen, doCleanup)

	// Setup fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		colorPrintf(colorYellow, "[WATCH] fsnotify unavailable (%v), falling back to polling\n", err)
		watchPoll(cfg, fallbackInterval, seen, doCleanup)
		return
	}
	defer watcher.Close()

	// Watch source dir (and all subdirs if recursive)
	addWatchDirs(watcher, cfg.GamesDir, cfg.Recursive)

	colorPrintf(colorCyan, "\nWatching %s — Ctrl+C to stop\n", cfg.GamesDir)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Fallback ticker: catches files missed on NAS/network mounts
	ticker := time.NewTicker(fallbackInterval)
	defer ticker.Stop()

	// Debounce: accumulate new paths, flush 2s after last event
	pending := map[string]bool{}
	debounce := time.NewTimer(0)
	<-debounce.C // drain initial tick

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				info, err := os.Stat(event.Name)
				if err != nil {
					continue
				}
				if info.IsDir() {
					if cfg.Recursive {
						// New directory: watch it and scan for game files
						addWatchDirs(watcher, event.Name, false)
						for _, f := range collectGameFiles(event.Name, false) {
							pending[f] = true
						}
					}
				} else if gameExts[strings.ToLower(filepath.Ext(event.Name))] {
					pending[event.Name] = true
				}
				debounce.Reset(2 * time.Second)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			colorPrintf(colorRed, "[WATCH] watcher error: %v\n", err)
		case <-debounce.C:
			flushPending(cfg, pending, seen, doCleanup)
		case <-ticker.C:
			// Fallback scan for network mounts where events may be missed
			for _, f := range collectGameFiles(cfg.GamesDir, cfg.Recursive) {
				if !seen[f] {
					pending[f] = true
				}
			}
			if len(pending) > 0 {
				flushPending(cfg, pending, seen, doCleanup)
			}
		case <-sigCh:
			colorPrint(colorCyan, "\nWatch stopped.\n")
			return
		}
	}
}

// addWatchDirs adds dir to the watcher. If deep is true, all subdirectories are
// added recursively (skipping hidden dirs).
func addWatchDirs(w *fsnotify.Watcher, dir string, deep bool) {
	_ = w.Add(dir)
	if !deep {
		return
	}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		_ = w.Add(path)
		return nil
	})
}

// flushPending processes accumulated new paths and clears the pending map.
func flushPending(cfg *Config, pending map[string]bool, seen map[string]bool, doCleanup bool) {
	var newFiles []string
	for p := range pending {
		if !seen[p] {
			newFiles = append(newFiles, p)
		}
	}
	for k := range pending {
		delete(pending, k)
	}
	if len(newFiles) == 0 {
		return
	}
	sort.Strings(newFiles)
	colorPrintf(colorCyan, "\n[WATCH] %d nouveau(x) fichier(s) détecté(s)\n", len(newFiles))
	processWatchBatch(cfg, newFiles, seen, doCleanup)
}

// watchPoll is the pure-polling fallback used when fsnotify cannot be initialised.
func watchPoll(cfg *Config, interval time.Duration, seen map[string]bool, doCleanup bool) {
	colorPrintf(colorCyan, "\nWatching %s (polling every %s) — Ctrl+C to stop\n",
		cfg.GamesDir, interval)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			var newFiles []string
			for _, f := range collectGameFiles(cfg.GamesDir, cfg.Recursive) {
				if !seen[f] {
					newFiles = append(newFiles, f)
				}
			}
			if len(newFiles) > 0 {
				colorPrintf(colorCyan, "\n[WATCH] %d nouveau(x) fichier(s) détecté(s)\n", len(newFiles))
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
