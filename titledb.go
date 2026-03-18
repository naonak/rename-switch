package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	titledbUS  = "https://github.com/blawar/titledb/raw/refs/heads/master/US.en.json"
	titledbGB  = "https://github.com/blawar/titledb/raw/refs/heads/master/GB.en.json"
	titledbVer = "https://github.com/blawar/titledb/raw/refs/heads/master/versions.json"
)

// TitleDB holds indexed title names and version history.
type TitleDB struct {
	// names maps lowercase titleid → official name
	names map[string]string
	// versions maps lowercase titleid → map[versionNum]date
	versions map[string]map[string]string
}

// LoadTitleDB loads the titledb from cache. Returns error if cache files are missing.
func LoadTitleDB(cacheDir string) (*TitleDB, error) {
	namesPath := filepath.Join(cacheDir, "titledb_names.json")
	versionsPath := filepath.Join(cacheDir, "titledb_versions.json")

	db := &TitleDB{}

	namesData, err := os.ReadFile(namesPath)
	if err != nil {
		return nil, fmt.Errorf("titledb cache not found (%s): run with -update-db first", namesPath)
	}
	if err := json.Unmarshal(namesData, &db.names); err != nil {
		return nil, fmt.Errorf("corrupt names cache: %v", err)
	}

	versionsData, err := os.ReadFile(versionsPath)
	if err != nil {
		return nil, fmt.Errorf("titledb versions cache not found: %v", err)
	}
	if err := json.Unmarshal(versionsData, &db.versions); err != nil {
		return nil, fmt.Errorf("corrupt versions cache: %v", err)
	}

	return db, nil
}

// Update downloads titledb from GitHub and writes to cacheDir.
func (db *TitleDB) Update(cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	names := make(map[string]string)

	for _, url := range []string{titledbUS, titledbGB} {
		colorPrintf(colorCyan, "  Fetching %s...\n", filepath.Base(url))
		raw, err := fetchJSON(client, url)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", url, err)
		}
		// Each JSON is an object: key → {id, name, ...}
		var catalog map[string]json.RawMessage
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return fmt.Errorf("parse %s: %w", url, err)
		}
		for _, v := range catalog {
			var entry struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}
			tid := strings.ToLower(entry.ID)
			if tid == "" || entry.Name == "" {
				continue
			}
			// US takes priority over GB (first wins)
			if _, exists := names[tid]; !exists {
				names[tid] = entry.Name
			}
		}
	}
	colorPrintf(colorCyan, "  %d titles indexed.\n", len(names))

	colorPrintf(colorCyan, "  Fetching versions.json...\n")
	versionsRaw, err := fetchJSON(client, titledbVer)
	if err != nil {
		return fmt.Errorf("fetch versions.json: %w", err)
	}
	var versionsRaw2 map[string]map[string]string
	if err := json.Unmarshal(versionsRaw, &versionsRaw2); err != nil {
		return fmt.Errorf("parse versions.json: %w", err)
	}
	versions := make(map[string]map[string]string, len(versionsRaw2))
	for k, v := range versionsRaw2 {
		versions[strings.ToLower(k)] = v
	}
	colorPrintf(colorCyan, "  %d titles with version history.\n", len(versions))

	// Write caches
	if err := writeJSON(filepath.Join(cacheDir, "titledb_names.json"), names); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cacheDir, "titledb_versions.json"), versions); err != nil {
		return err
	}

	db.names = names
	db.versions = versions
	return nil
}

// LookupName returns the official game name for a titleID (lowercase), or "" if not found.
func (db *TitleDB) LookupName(titleID string) string {
	return db.names[strings.ToLower(titleID)]
}

// LatestVersion returns the latest known version string (e.g. "v131072") for a titleID,
// or "" if not found. Uses the update TitleID (x800) for lookups.
func (db *TitleDB) LatestVersion(titleID string) string {
	tid := strings.ToLower(titleID)
	vmap, ok := db.versions[tid]
	if !ok || len(vmap) == 0 {
		return ""
	}
	// Find the highest version number
	var best int64 = -1
	for k := range vmap {
		n, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		if n > best {
			best = n
		}
	}
	if best < 0 {
		return ""
	}
	return fmt.Sprintf("v%d", best)
}

func fetchJSON(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return buf, nil
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
