package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config holds runtime configuration passed to ProcessFile.
type Config struct {
	Apply      bool
	GamesDir   string
	NstoolPath string
	DB         *TitleDB
}

var (
	reTitleID    = regexp.MustCompile(`(?i)0100[0-9A-Fa-f]{12}`)
	reVersionBracket = regexp.MustCompile(`(?i)[\[\(]v(\d+)[\]\)]`)
	reVersionNum     = regexp.MustCompile(`\[(\d{5,})\]`) // bare number ≥5 digits
)

// ProcessFile renames (or previews rename of) a single Switch game file.
// Returns nil on success (including "already correctly named"), error otherwise.
func ProcessFile(cfg *Config, path string) error {
	filename := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(filename))
	ext = strings.TrimPrefix(ext, ".")

	titleID, version, method := "", "", "FAST"

	// ── Fast path: TitleID in filename ──────────────────────────────────────────
	if m := reTitleID.FindString(filename); m != "" {
		titleID = strings.ToLower(m)

		// Extract version [v12345] or (v12345)
		if vm := reVersionBracket.FindStringSubmatch(filename); vm != nil {
			version = "v" + vm[1]
		}
		// Fallback: bare number in brackets [262144]
		if version == "" {
			if vm := reVersionNum.FindStringSubmatch(filename); vm != nil {
				version = "v" + vm[1]
			}
		}
	} else {
		// ── Slow path: nstool ──────────────────────────────────────────────────
		method = "SLOW"
		meta, err := ExtractMeta(cfg.NstoolPath, path)
		if err != nil {
			colorPrintf(colorRed, "  [ERROR] Cannot read metadata: %s (%v)\n", filename, err)
			return fmt.Errorf("nstool: %w", err)
		}
		titleID = meta.TitleID
		version = meta.Version
	}

	if titleID == "" {
		colorPrintf(colorRed, "  [ERROR] No TitleID found: %s\n", filename)
		return fmt.Errorf("no titleID")
	}

	// ── Type from TitleID ────────────────────────────────────────────────────────
	gtype := GetType(titleID)

	// ── Version fallback via versions.json ─────────────────────────────────────
	if version == "" {
		vtid := titleID
		if gtype == "BASE" {
			// BASE: look up the update TitleID (x000 → x800) to confirm game exists
			vtid = titleID[:13] + "800"
		}
		if latest := cfg.DB.LatestVersion(vtid); latest != "" {
			if gtype == "BASE" {
				version = "v0"
			} else {
				version = latest
			}
		}
	}
	if version == "" {
		version = "v0"
	}

	// ── Name from titledb ────────────────────────────────────────────────────────
	name := cfg.DB.LookupName(titleID)
	if name == "" && gtype != "BASE" {
		// UPD/DLC: try base TitleID (x000)
		name = cfg.DB.LookupName(titleID[:13] + "000")
	}
	if name == "" {
		name = CleanFilenameTitle(filename)
	}
	name = SanitizeName(name)
	if name == "" {
		name = "Unknown"
	}

	// ── Build new filename ───────────────────────────────────────────────────────
	newName := fmt.Sprintf("%s [%s][%s][%s].%s", name, gtype, titleID, version, ext)

	// Handle duplicates: check if new path already exists (and is a different file)
	newPath := filepath.Join(cfg.GamesDir, newName)
	if _, err := os.Stat(newPath); err == nil && newPath != path {
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s [%s][%s][%s]_%d.%s", name, gtype, titleID, version, i, ext)
			candidatePath := filepath.Join(cfg.GamesDir, candidate)
			if _, err := os.Stat(candidatePath); os.IsNotExist(err) {
				newName = candidate
				newPath = candidatePath
				break
			}
		}
	}

	// ── Output ───────────────────────────────────────────────────────────────────
	if filename == newName {
		colorPrintf(colorGray, "  [OK]   %s\n", filename)
		return nil
	}

	methodColor := colorGreen
	if method == "SLOW" {
		methodColor = colorYellow
	}
	colorPrintf(methodColor, "  [%s] %s\n", method, filename)
	colorPrintf(colorCyan, "       → %s\n", newName)

	if cfg.Apply {
		if err := os.Rename(path, newPath); err != nil {
			colorPrintf(colorRed, "  [ERROR] rename failed: %v\n", err)
			return err
		}
	}
	return nil
}

// GetType returns "BASE", "UPD", or "DLC" based on the last 3 hex nibbles of titleID.
func GetType(titleID string) string {
	if len(titleID) < 3 {
		return "BASE"
	}
	suffix := strings.ToLower(titleID[len(titleID)-3:])
	switch suffix {
	case "000":
		return "BASE"
	case "800":
		return "UPD"
	default:
		return "DLC"
	}
}

// SanitizeName removes or replaces characters invalid in filenames.
func SanitizeName(name string) string {
	// Replace path separators
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	// ": " → " - " (e.g. "Fire Emblem: Three Houses" → "Fire Emblem - Three Houses")
	name = strings.ReplaceAll(name, ": ", " - ")
	// Remove trailing colon
	name = strings.TrimSuffix(name, ":")
	// Remove characters invalid on most filesystems
	for _, ch := range []string{"*", "?", "\"", "<", ">", "|"} {
		name = strings.ReplaceAll(name, ch, "")
	}
	// Collapse multiple spaces
	for strings.Contains(name, "  ") {
		name = strings.ReplaceAll(name, "  ", " ")
	}
	return strings.TrimSpace(name)
}

var (
	reTitleIDBlock  = regexp.MustCompile(`(?i)\[0100[0-9A-Fa-f]+\].*`)
	reSceneTags     = regexp.MustCompile(`(?i)[ _-]+(CLC|SUXXORS|VENOM|Nufan|NiiNTENDO|MBC|BigBlueBox|Ziperto|Proper)`)
	reFormatTags    = regexp.MustCompile(`(?i)[ _]+(Super)?(XC[Iz]|NS[Pz])`)
	reRegionTags    = regexp.MustCompile(`[ _]+(Eur|EUR|US|USA|JAP|All|As|MULTi5?)(\b|$)`)
	reVersionStr    = regexp.MustCompile(`[ _][Vv]\d+(\.\d+)*`)
	reScenePrefix   = regexp.MustCompile(`(?i)^(v|sxs|n|bbb|venom|suxxors|clc)-`)
	reSeparators    = regexp.MustCompile(`^[ _-]+|[ _-]+$`)
)

// CleanFilenameTitle extracts a human-readable game name from a raw filename
// when the titledb lookup fails.
func CleanFilenameTitle(filename string) string {
	// Remove extension
	base := filename
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}

	// Remove TitleID block and everything after
	base = reTitleIDBlock.ReplaceAllString(base, "")

	// Remove scene release tags
	base = reSceneTags.ReplaceAllString(base, "")

	// Remove format tags (XCI, NSP, NSZ, XCZ, SuperXCI, etc.)
	base = reFormatTags.ReplaceAllString(base, "")

	// Remove region tags
	base = reRegionTags.ReplaceAllString(base, "")

	// Remove version strings like v1.2.3, V131072
	base = reVersionStr.ReplaceAllString(base, "")

	// Remove scene prefixes like "v-", "sxs-", "clc-"
	base = reScenePrefix.ReplaceAllString(base, "")

	// Replace underscores with spaces
	base = strings.ReplaceAll(base, "_", " ")

	// Trim leading/trailing separators
	base = reSeparators.ReplaceAllString(base, "")

	// Collapse multiple spaces
	for strings.Contains(base, "  ") {
		base = strings.ReplaceAll(base, "  ", " ")
	}

	return strings.TrimSpace(base)
}
