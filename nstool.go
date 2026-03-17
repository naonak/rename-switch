package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// NSTMeta contains the metadata extracted from a Switch game file.
type NSTMeta struct {
	TitleID       string // 16 hex chars, lowercase
	Version       string // "v0", "v131072", etc. Empty if unknown.
	UpdateVersion string // version of the bundled update, "" if none
	DLCCount      int    // number of bundled DLC CNMTs
}

var (
	reHash32      = regexp.MustCompile(`[0-9a-f]{32}`)
	reCnmtInner   = regexp.MustCompile(`([A-Za-z]+)_([0-9a-f]{16})\.cnmt`)
	reProgID      = regexp.MustCompile(`ProgID:\s*0x([0-9a-f]{16})`)
	reVersionParen = regexp.MustCompile(`\(v(\d+)\)`)
)

// ExtractMeta uses nstool to extract TitleID and Version from an NSP or XCI file.
// Returns an error if nstool is unavailable or the file cannot be parsed.
func ExtractMeta(nstoolPath, filePath string) (*NSTMeta, error) {
	// Verify nstool exists
	if _, err := exec.LookPath(nstoolPath); err != nil {
		if _, err2 := os.Stat(nstoolPath); err2 != nil {
			return nil, fmt.Errorf("nstool not found at %q", nstoolPath)
		}
	}

	tmpDir, err := os.MkdirTemp("", "rename-switch-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: get fstree
	tree, err := runNstool(nstoolPath, "--fstree", filePath)
	if err != nil {
		return nil, fmt.Errorf("nstool fstree: %w", err)
	}

	isXCI := strings.Contains(tree, "gamecard:/")

	// Step 2: find cnmt.nca candidates
	candidates := findCnmtCandidates(tree, isXCI)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no cnmt.nca found in %s", filepath.Base(filePath))
	}

	// Step 3: iterate all candidates, tracking BASE, UPDATE and DLC counts
	type candidate struct {
		innerName string
		hash      string
		priority  int // 1=BASE, 2=UPD, 3=DLC
	}
	var best, updateCand *candidate
	dlcCount := 0

	for _, hash := range candidates {
		cpath := "/" + hash + ".cnmt.nca"
		if isXCI {
			cpath = "/secure/" + hash + ".cnmt.nca"
		}

		tryNca := filepath.Join(tmpDir, "cnmt_try.nca")
		_ = os.Remove(tryNca)

		_, _ = runNstool(nstoolPath, "-x", cpath, tryNca, filePath)
		if _, err := os.Stat(tryNca); err != nil {
			continue
		}

		ncaTree, _ := runNstool(nstoolPath, "--fstree", tryNca)

		innerName := ""
		if m := reCnmtInner.FindStringSubmatch(ncaTree); m != nil {
			innerName = m[0]
		}

		// Fallback: read ProgID from NCA header (keygen ≥ 18 — inner cnmt unreadable)
		if innerName == "" {
			if m := reProgID.FindStringSubmatch(ncaTree); m != nil {
				progID := m[1]
				suffix := progID[len(progID)-3:]
				switch suffix {
				case "000":
					innerName = "Application_" + progID + ".cnmt"
				case "800":
					innerName = "Patch_" + progID + ".cnmt"
				default:
					innerName = "AddOnContent_" + progID + ".cnmt"
				}
			}
		}
		if innerName == "" {
			continue
		}

		priority := 3
		lower := strings.ToLower(innerName)
		if strings.HasPrefix(lower, "application_") {
			priority = 1
		} else if strings.HasPrefix(lower, "patch_") {
			priority = 2
		}

		// Save update NCA (copy before potential rename below)
		if priority == 2 && updateCand == nil {
			updateCand = &candidate{innerName: innerName, hash: hash, priority: 2}
			_ = copyFile(tryNca, filepath.Join(tmpDir, "update.cnmt.nca"))
		}
		if priority == 3 {
			dlcCount++
		}

		if best == nil || priority < best.priority {
			best = &candidate{innerName: innerName, hash: hash, priority: priority}
			if err := os.Rename(tryNca, filepath.Join(tmpDir, "cnmt.nca")); err != nil {
				_ = copyFile(tryNca, filepath.Join(tmpDir, "cnmt.nca"))
			}
		}
	}

	if best == nil {
		return nil, fmt.Errorf("could not determine TitleID from %s", filepath.Base(filePath))
	}

	// Step 4: extract TitleID from inner cnmt filename
	m := reCnmtInner.FindStringSubmatch(best.innerName)
	if m == nil {
		return nil, fmt.Errorf("unexpected inner cnmt name: %s", best.innerName)
	}
	titleID := m[2]

	// Step 5: extract version from inner .cnmt
	version := ""
	cnmtNca := filepath.Join(tmpDir, "cnmt.nca")
	metaCnmt := filepath.Join(tmpDir, "meta.cnmt")
	innerPath := "/0/" + best.innerName
	_, _ = runNstool(nstoolPath, "-x", innerPath, metaCnmt, cnmtNca)
	if _, err := os.Stat(metaCnmt); err == nil {
		out, _ := runNstool(nstoolPath, "-t", "cnmt", "-v", metaCnmt)
		if vm := reVersionParen.FindStringSubmatch(out); vm != nil {
			version = "v" + vm[1]
		}
	}

	// Step 6: extract version from bundled update CNMT (if any)
	updateVersion := ""
	if updateCand != nil {
		updateNca := filepath.Join(tmpDir, "update.cnmt.nca")
		updateMetaCnmt := filepath.Join(tmpDir, "update.meta.cnmt")
		_, _ = runNstool(nstoolPath, "-x", "/0/"+updateCand.innerName, updateMetaCnmt, updateNca)
		if _, err := os.Stat(updateMetaCnmt); err == nil {
			out, _ := runNstool(nstoolPath, "-t", "cnmt", "-v", updateMetaCnmt)
			if vm := reVersionParen.FindStringSubmatch(out); vm != nil {
				updateVersion = "v" + vm[1]
			}
		}
	}

	return &NSTMeta{
		TitleID:       titleID,
		Version:       version,
		UpdateVersion: updateVersion,
		DLCCount:      dlcCount,
	}, nil
}

// findCnmtCandidates parses nstool fstree output and returns 32-char hex hashes
// of cnmt.nca files. For XCI, only looks inside secure/ partition.
func findCnmtCandidates(tree string, isXCI bool) []string {
	var candidates []string
	seen := map[string]bool{}

	if isXCI {
		inSecure := false
		for _, line := range strings.Split(tree, "\n") {
			trimmed := strings.TrimRight(line, "\r")
			// Detect partition headers (two-space indent + name + /)
			if strings.HasPrefix(trimmed, "  ") && strings.HasSuffix(trimmed, "/") && !strings.HasPrefix(trimmed, "   ") {
				partName := strings.TrimSpace(trimmed)
				inSecure = partName == "secure/"
				continue
			}
			if !inSecure {
				continue
			}
			if strings.Contains(trimmed, ".cnmt.nca") {
				if m := reHash32.FindString(trimmed); m != "" && !seen[m] {
					seen[m] = true
					candidates = append(candidates, m)
				}
			}
		}
	} else {
		for _, line := range strings.Split(tree, "\n") {
			if strings.Contains(line, ".cnmt.nca") {
				if m := reHash32.FindString(line); m != "" && !seen[m] {
					seen[m] = true
					candidates = append(candidates, m)
				}
			}
		}
	}
	return candidates
}

// runNstool executes nstool with given args and returns combined stdout+stderr.
func runNstool(nstoolPath string, args ...string) (string, error) {
	cmd := exec.Command(nstoolPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
