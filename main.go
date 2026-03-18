package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var version = "dev"

const helpText = `rename-switch — Nintendo Switch game file renamer

Usage:
  rename-switch [options] [files...]

Options:
  -apply                Apply renames (default: dry-run, only shows what would change)
  -cleanup              Remove redundant UPD/BASE files after renaming (respects dry-run)
  -prune-empty          Remove empty directories left after renaming or moving files
  -watch                Watch source directory and process new files automatically
  -watch-interval DUR   Fallback scan interval for -watch mode (default: 60s, e.g. 30s, 5m)
  -update-db            Refresh titledb cache from blawar/titledb
  -src DIR              Directory containing game files (default: current directory)
  -nstool PATH          Path to nstool binary (default: searches PATH, then /usr/local/bin/nstool)
  -dest DIR             Destination directory for renamed files (default: same directory as source)
  -recursive            Scan subdirectories recursively
  -version              Show version
  -h, -help             Show this help

Arguments:
  files           One or more filenames to process (basename or full path).
                  If omitted, all .nsp/.xci/.nsz/.xcz files in -src DIR are processed.

Examples:
  rename-switch                                       # dry-run on all files
  rename-switch -apply                                # apply all renames
  rename-switch game.nsp                              # dry-run on one file
  rename-switch -apply game.nsp update.nsp dlc.nsp    # apply on specific files
  rename-switch -src /mnt/games -apply                # specify source directory
  rename-switch -src /mnt/games -dest /mnt/out -apply # move renamed files to /mnt/out
  rename-switch -update-db                            # refresh titledb

Output format:
  [FAST] filename.nsp           ← TitleID found in filename (fast path)
         → New Name [TYPE][titleid][vVERSION].nsp
  [SLOW] filename.xci           ← TitleID extracted via nstool (slow path)
         → New Name [TYPE][titleid][vVERSION].xci
  [OK]   filename.nsp           ← already correctly named, no change needed

Title types:
  BASE  — base game (TitleID ends in 000)
  UPD   — update/patch (TitleID ends in 800)
  DLC   — downloadable content (all others)

Errors are written to _errors.log in the source directory (only in -apply mode).
`

func main() {
	var (
		apply         bool
		updateDB      bool
		recursive     bool
		cleanup       bool
		pruneEmpty    bool
		watch         bool
		watchInterval time.Duration
		gamesDir      string
		destDir       string
		nstoolPath    string
		showVer       bool
	)

	flag.BoolVar(&apply, "apply", false, "Apply renames (default: dry-run)")
	flag.BoolVar(&updateDB, "update-db", false, "Refresh titledb cache")
	flag.BoolVar(&recursive, "recursive", false, "Scan subdirectories recursively")
	flag.BoolVar(&cleanup, "cleanup", false, "Remove redundant UPD/BASE files after renaming")
	flag.BoolVar(&pruneEmpty, "prune-empty", false, "Remove empty directories left after renaming or moving files")
	flag.BoolVar(&watch, "watch", false, "Watch source directory and process new files automatically")
	flag.DurationVar(&watchInterval, "watch-interval", 60*time.Second, "Fallback scan interval for -watch mode (e.g. 30s, 5m)")
	flag.StringVar(&gamesDir, "src", ".", "Source directory")
	flag.StringVar(&destDir, "dest", "", "Destination directory for renamed files (default: same dir as source)")
	flag.StringVar(&nstoolPath, "nstool", "", "Path to nstool binary")
	flag.BoolVar(&showVer, "version", false, "Show version")
	flag.Usage = func() { fmt.Print(helpText) }
	flag.Parse()

	if showVer {
		fmt.Printf("rename-switch %s\n", version)
		return
	}

	// Resolve source directory
	var err error
	gamesDir, err = filepath.Abs(gamesDir)
	if err != nil {
		fatalf("invalid -src directory: %v\n", err)
	}
	if info, err := os.Stat(gamesDir); err != nil || !info.IsDir() {
		fatalf("source directory does not exist: %s\n", gamesDir)
	}

	// Resolve nstool
	if nstoolPath == "" {
		nstoolPath = findNstool()
	}

	// Cache dir for titledb
	cacheDir := filepath.Join(homeDir(), ".switch")

	// Load or update titledb
	if updateDB {
		colorPrint(colorCyan, "Updating titledb...\n")
		db := &TitleDB{}
		if err := db.Update(cacheDir); err != nil {
			fatalf("titledb update failed: %v\n", err)
		}
		colorPrint(colorGreen, "Database updated.\n")
		return
	}

	db, err := LoadTitleDB(cacheDir)
	if err != nil {
		fatalf("failed to load titledb: %v\nRun with -update-db to download it.\n", err)
	}

	// Resolve dest directory
	if destDir != "" {
		destDir, err = filepath.Abs(destDir)
		if err != nil {
			fatalf("invalid -dest directory: %v\n", err)
		}
		if err := os.MkdirAll(destDir, 0755); err != nil {
			fatalf("cannot create -dest directory: %v\n", err)
		}
	}

	cfg := &Config{
		Apply:      apply,
		GamesDir:   gamesDir,
		DestDir:    destDir,
		NstoolPath: nstoolPath,
		Recursive:  recursive,
		PruneEmpty: pruneEmpty,
		DB:         db,
	}

	// Print mode header
	if apply {
		colorPrint(colorGreen, "=== APPLYING RENAMES ===\n")
	} else {
		colorPrint(colorYellow, "=== DRY RUN (use -apply to rename) ===\n")
	}

	// Clear errors log in apply mode
	errorsLog := filepath.Join(gamesDir, "_errors.log")
	if apply {
		_ = os.WriteFile(errorsLog, nil, 0644)
	}

	// Collect target files
	targets := flag.Args()

	var files []string
	if len(targets) > 0 {
		for _, t := range targets {
			// Accept both basename and full path
			p := t
			if !filepath.IsAbs(p) {
				local := filepath.Join(gamesDir, p)
				if _, err := os.Stat(local); err == nil {
					p = local
				}
			}
			if _, err := os.Stat(p); err != nil {
				colorPrintf(colorRed, "File not found: %s\n", t)
				continue
			}
			files = append(files, p)
		}
	} else {
		files = collectGameFiles(gamesDir, recursive)
	}

	// ── Watch mode ───────────────────────────────────────────────────────────────
	if watch {
		Watch(cfg, watchInterval, cleanup)
		return
	}

	// ── Normal mode ───────────────────────────────────────────────────────────
	count, errors := 0, 0
	for _, f := range files {
		if err := ProcessFile(cfg, f); err != nil {
			errors++
			if apply {
				appendLine(errorsLog, filepath.Base(f))
			}
		} else {
			count++
		}
	}

	if len(targets) == 0 {
		fmt.Println()
		colorPrintf(colorCyan, "Processed: %d files\n", count)
		if errors > 0 {
			colorPrintf(colorRed, "Errors: %d (see %s)\n", errors, errorsLog)
		}
		if !apply {
			colorPrint(colorYellow, "Run with -apply to execute renames.\n")
		}
	}

	if cleanup {
		cleanupDir := destDir
		if cleanupDir == "" {
			cleanupDir = gamesDir
		}
		Cleanup(cleanupDir, nstoolPath, apply)
	}

	if pruneEmpty {
		PruneEmptyDirs(gamesDir, apply)
	}
}

// collectGameFiles returns all .nsp/.xci/.nsz/.xcz files in dir, skipping hidden files.
// If recursive is true, subdirectories are scanned as well.
func collectGameFiles(dir string, recursive bool) []string {
	exts := map[string]bool{".nsp": true, ".xci": true, ".nsz": true, ".xcz": true}
	var files []string

	if recursive {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() && exts[strings.ToLower(filepath.Ext(d.Name()))] {
				files = append(files, path)
			}
			return nil
		})
	} else {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if exts[strings.ToLower(filepath.Ext(e.Name()))] {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}

	sort.Strings(files)
	return files
}

func findNstool() string {
	// Check common locations
	candidates := []string{"/usr/local/bin/nstool", "/usr/bin/nstool"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Will rely on PATH via exec.LookPath in nstool.go
	return "nstool"
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return h
}

func fatalf(format string, args ...any) {
	colorPrintf(colorRed, format, args...)
	os.Exit(1)
}

func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
