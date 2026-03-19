package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var reCanonical = regexp.MustCompile(
	`\[(BASE|UPD|DLC)\]\[([0-9a-f]{16})\]\[v(\d+)\]` +
		`(?:\[\+UPD v(\d+)\])?(?:\[\+(\d+) DLC\])?`,
)

// cleanFile holds parsed info from a canonical filename.
type cleanFile struct {
	Path           string
	Name           string   // display name
	Type           string   // BASE, UPD, DLC
	TitleID        string   // as found in filename
	BaseTitleID    string   // titleID[:13]+"000"
	Version        int64    // numeric version
	BundledUpdateV int64    // -1 if no bundled update
	BundledDLCCnt  int      // count of bundled DLCs (from filename)
	BundledDLCIDs  []string // actual DLC titleIDs (populated lazily via nstool)
	Size           int64    // file size in bytes
}

func parseCleanFile(path string) *cleanFile {
	name := filepath.Base(path)
	m := reCanonical.FindStringSubmatch(name)
	if m == nil {
		return nil
	}

	ftype := m[1]
	titleID := m[2]
	ver, _ := strconv.ParseInt(m[3], 10, 64)

	bundledUpd := int64(-1)
	if m[4] != "" {
		bundledUpd, _ = strconv.ParseInt(m[4], 10, 64)
	}

	dlcCnt := 0
	if m[5] != "" {
		dlcCnt, _ = strconv.Atoi(m[5])
	}

	baseTID := titleID[:13] + "000"

	cf := &cleanFile{
		Path:           path,
		Name:           name,
		Type:           ftype,
		TitleID:        titleID,
		BaseTitleID:    baseTID,
		Version:        ver,
		BundledUpdateV: bundledUpd,
		BundledDLCCnt:  dlcCnt,
	}
	if info, err := os.Stat(path); err == nil {
		cf.Size = info.Size()
	}
	return cf
}

// formatSize returns a human-readable file size (e.g. "3.6 GB").
func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Cleanup scans dir, identifies redundant game files, and prints (or deletes) them.
func Cleanup(dir, nstoolPath string, apply bool) {
	exts := map[string]bool{".nsp": true, ".xci": true, ".nsz": true, ".xcz": true}

	entries, err := os.ReadDir(dir)
	if err != nil {
		colorPrintf(colorRed, "  [ERROR] Cannot read directory: %v\n", err)
		return
	}

	// Parse all game files
	var files []*cleanFile
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !exts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		if cf := parseCleanFile(filepath.Join(dir, e.Name())); cf != nil {
			files = append(files, cf)
		}
	}

	// Group by baseTitleID
	type group struct {
		bases []*cleanFile
		upds  []*cleanFile
		dlcs  []*cleanFile
	}
	groups := map[string]*group{}
	for _, f := range files {
		g := groups[f.BaseTitleID]
		if g == nil {
			g = &group{}
			groups[f.BaseTitleID] = g
		}
		switch f.Type {
		case "BASE":
			g.bases = append(g.bases, f)
		case "UPD":
			g.upds = append(g.upds, f)
		case "DLC":
			g.dlcs = append(g.dlcs, f)
		}
	}

	// Collect all DLC titleIDs from standalone DLC files in a group
	standaloneDLCIDs := func(g *group) map[string]bool {
		ids := map[string]bool{}
		for _, d := range g.dlcs {
			ids[d.TitleID] = true
		}
		return ids
	}

	// Resolve DLC titleIDs for a file via nstool (lazy, only when needed)
	resolveDLCIDs := func(f *cleanFile) []string {
		if f.BundledDLCCnt == 0 {
			return nil
		}
		if f.BundledDLCIDs != nil {
			return f.BundledDLCIDs
		}
		meta, err := ExtractMeta(nstoolPath, f.Path)
		if err != nil || len(meta.DLCTitleIDs) == 0 {
			return nil
		}
		f.BundledDLCIDs = meta.DLCTitleIDs
		return f.BundledDLCIDs
	}

	// isSubset checks if all IDs in 'a' are in 'b'
	isSubset := func(a []string, b map[string]bool) bool {
		for _, id := range a {
			if !b[id] {
				return false
			}
		}
		return true
	}

	// Collect all DLC IDs covered by a set of BASE files + standalone DLCs
	coveredDLCIDs := func(bases []*cleanFile, standalone map[string]bool) map[string]bool {
		covered := map[string]bool{}
		for id := range standalone {
			covered[id] = true
		}
		for _, b := range bases {
			for _, id := range b.BundledDLCIDs {
				covered[id] = true
			}
		}
		return covered
	}

	type candidate struct {
		file   *cleanFile
		reason string
	}
	var toDelete []candidate
	var toSkip []candidate

	// Sort baseTitleIDs for deterministic output
	var baseTIDs []string
	for tid := range groups {
		baseTIDs = append(baseTIDs, tid)
	}
	sort.Strings(baseTIDs)

	for _, tid := range baseTIDs {
		g := groups[tid]
		standaloneDLCs := standaloneDLCIDs(g)

		// ── Rule 1 & 2: UPD standalone ─────────────────────────────────────────
		// Find max bundled update version across all BASE files
		maxBundled := int64(-1)
		var bestBaseForBundle *cleanFile
		for _, b := range g.bases {
			if b.BundledUpdateV > maxBundled {
				maxBundled = b.BundledUpdateV
				bestBaseForBundle = b
			}
		}

		// Sort UPDs descending by version
		sort.Slice(g.upds, func(i, j int) bool {
			return g.upds[i].Version > g.upds[j].Version
		})

		for i, upd := range g.upds {
			var reason string
			if upd.Version <= maxBundled && maxBundled >= 0 {
				reason = fmt.Sprintf("covered by bundled update [+UPD v%d] in %s",
					maxBundled, filepath.Base(bestBaseForBundle.Path))
			} else if i > 0 {
				reason = fmt.Sprintf("outdated (v%d available)", g.upds[0].Version)
			}
			if reason == "" {
				continue
			}
			// UPD files never have bundled DLCs, no exception needed
			toDelete = append(toDelete, candidate{upd, reason})
		}

		// ── Rule 3: multiple BASE files ─────────────────────────────────────────
		if len(g.bases) <= 1 {
			continue
		}

		// Sort BASE files: highest bundled update first, then highest DLC count
		sort.Slice(g.bases, func(i, j int) bool {
			if g.bases[i].BundledUpdateV != g.bases[j].BundledUpdateV {
				return g.bases[i].BundledUpdateV > g.bases[j].BundledUpdateV
			}
			return g.bases[i].BundledDLCCnt > g.bases[j].BundledDLCCnt
		})

		best := g.bases[0]
		// Pre-resolve DLC IDs for the best file (needed for comparison)
		resolveDLCIDs(best)
		bestDLCIDs := coveredDLCIDs([]*cleanFile{best}, standaloneDLCs)

		for _, base := range g.bases[1:] {
			if base.BundledDLCCnt == 0 {
				// No bundled DLCs: safe to delete if best has same or newer update
				toDelete = append(toDelete, candidate{base,
					fmt.Sprintf("replaced by %s", filepath.Base(best.Path))})
				continue
			}

			// Has bundled DLCs: resolve and compare
			dlcIDs := resolveDLCIDs(base)
			if dlcIDs == nil {
				// Cannot resolve: keep it to be safe
				toSkip = append(toSkip, candidate{base, "bundled DLCs unverifiable (nstool unavailable)"})
				continue
			}
			if isSubset(dlcIDs, bestDLCIDs) {
				toDelete = append(toDelete, candidate{base,
					fmt.Sprintf("replaced by %s", filepath.Base(best.Path))})
			} else {
				var unique []string
				for _, id := range dlcIDs {
					if !bestDLCIDs[id] {
						unique = append(unique, id)
					}
				}
				toSkip = append(toSkip, candidate{base,
					fmt.Sprintf("unique DLCs: [%s]", strings.Join(unique, ", "))})
			}
		}
	}

	// ── Output ─────────────────────────────────────────────────────────────────
	if apply {
		colorPrint(colorGreen, "\n=== CLEANUP ===\n")
	} else {
		colorPrint(colorYellow, "\n=== CLEANUP (dry run) ===\n")
	}

	if len(toDelete) == 0 && len(toSkip) == 0 {
		colorPrint(colorGray, "  No redundant files found.\n")
		return
	}

	for _, c := range toDelete {
		colorPrintf(colorRed, "  [DEL]  %s  (%s)\n", c.file.Name, formatSize(c.file.Size))
		colorPrintf(colorGray, "         → %s\n", c.reason)
		if apply {
			if err := os.Remove(c.file.Path); err != nil {
				colorPrintf(colorRed, "  [ERROR] %v\n", err)
			}
		}
	}

	for _, c := range toSkip {
		colorPrintf(colorYellow, "  [SKIP] %s\n", c.file.Name)
		colorPrintf(colorGray, "         → kept: %s\n", c.reason)
	}

	var totalBytes int64
	for _, c := range toDelete {
		totalBytes += c.file.Size
	}

	fmt.Println()
	if apply {
		colorPrintf(colorGreen, "%d file(s) deleted, %s freed", len(toDelete), formatSize(totalBytes))
	} else {
		colorPrintf(colorCyan, "%d file(s) to delete, %s will be freed", len(toDelete), formatSize(totalBytes))
	}
	if len(toSkip) > 0 {
		colorPrintf(colorCyan, " (%d kept despite partial redundancy)", len(toSkip))
	}
	fmt.Println()
	if !apply && len(toDelete) > 0 {
		colorPrint(colorYellow, "Run with -apply to execute.\n")
	}
}

// PruneEmptyDirs removes empty subdirectories under srcDir (bottom-up).
// The srcDir itself is never removed.
func PruneEmptyDirs(srcDir string, apply bool) {
	if apply {
		colorPrint(colorGreen, "\n=== PRUNE EMPTY DIRS ===\n")
	} else {
		colorPrint(colorYellow, "\n=== PRUNE EMPTY DIRS (dry run) ===\n")
	}

	// Collect all subdirectories
	var dirs []string
	filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == srcDir || !d.IsDir() {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})

	// Process bottom-up (deepest first) so nested empty dirs are caught
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	pruned := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			continue
		}
		colorPrintf(colorRed, "  [RMDIR] %s\n", dir)
		pruned++
		if apply {
			if err := os.Remove(dir); err != nil {
				colorPrintf(colorRed, "  [ERROR] %v\n", err)
			}
		}
	}

	if pruned == 0 {
		colorPrint(colorGray, "  No empty directories found.\n")
		return
	}
	fmt.Println()
	if apply {
		colorPrintf(colorGreen, "%d empty director(y/ies) removed\n", pruned)
	} else {
		colorPrintf(colorCyan, "%d empty director(y/ies) to remove\n", pruned)
		colorPrint(colorYellow, "Run with -apply to execute.\n")
	}
}
