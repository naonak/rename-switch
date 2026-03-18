# rename-switch

A CLI tool to rename Nintendo Switch game files (`.nsp`, `.xci`, `.nsz`, `.xcz`) with standardized names using official titles from [blawar/titledb](https://github.com/blawar/titledb) and metadata from [nstool](https://github.com/jakcron/nstool).

## Output format

```
Game Name [TYPE][titleid][vVERSION].ext
Game Name [BASE][titleid][vVERSION][+UPD vX][+N DLC].ext
```

Examples:
```
Metroid Prime Remastered [BASE][010012101468c000][v0].xci
Fire Emblem - Three Houses [BASE][010055d009f78000][v0][+UPD v196608][+6 DLC].xci
Blazing Beaks [UPD][010021a00de54800][v655360].nsp
```

Types: `BASE` (base game), `UPD` (update/patch), `DLC` (downloadable content)

When a BASE file bundles an update and/or DLC content, the suffix `[+UPD vX]` and/or `[+N DLC]` is appended automatically.

---

## Usage

### Without Docker (requires Go + nstool installed)

```bash
# Build
go build -o rename-switch .

# First run: download titledb (~50MB)
./rename-switch -update-db

# Dry-run on all files in current directory
./rename-switch

# Apply renames
./rename-switch -apply

# Target specific files
./rename-switch -apply game.nsp update.nsp

# Specify games directory
./rename-switch -src /mnt/switch-src -apply

# Custom nstool path
./rename-switch -nstool /usr/local/bin/nstool -apply
```

### With Docker (recommended)

```bash
# Pull the latest image
docker pull ghcr.io/naonak/rename-switch:latest

# Dry-run on your games directory
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch

# Apply renames
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch -apply

# Update titledb
docker run --rm \
  -v ~/.switch:/root/.switch \
  ghcr.io/naonak/rename-switch -update-db
```

> **Note:** Mount `~/.switch` to persist the titledb cache and provide Switch keys (`prod.keys`, `title.keys`) for nstool decryption.

#### Build from source

```bash
docker build -t rename-switch .
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  rename-switch -apply
```

---

## Cleanup — remove redundant files

The `-cleanup` flag identifies and optionally removes redundant files after renaming:

- **Standalone UPD covered by a bundled BASE** — e.g. if you have both `Game [BASE][...][+UPD v2]` and `Game [UPD][...][v2]`, the standalone UPD is redundant.
- **Outdated UPD files** — older update versions are removed when a newer one is present.
- **Duplicate BASE files** — the most complete version is kept (highest bundled update + most DLCs). A BASE with unique DLCs not present elsewhere is always kept.

Each file marked for deletion shows its size, and the total space freed is shown at the end.

```bash
# Preview what would be deleted (dry-run)
./rename-switch -cleanup

# Apply deletions
./rename-switch -apply -cleanup

# With Docker
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch -apply -cleanup
```

Example output:
```
=== CLEANUP (dry run) ===
  [DEL]  Mario Kart™ 8 Deluxe [UPD][...][v983040].nsp  (582.3 MB)
         → covered by bundled update [+UPD v1376256] in Mario Kart™ 8 Deluxe [BASE][...][v1376256][+UPD v1376256][+1 DLC].xci
  [DEL]  Fire Emblem™ - Three Houses [BASE][...][v0].nsp  (5.8 GB)
         → replaced by Fire Emblem™ - Three Houses [BASE][...][v0][+UPD v196608][+6 DLC].xci
  [SKIP] Game [BASE][...][v0][+2 DLC].xci
         → kept: unique DLCs: [0100xxxxxxxxxxxx01, 0100xxxxxxxxxxxx02]

12 file(s) to delete, 18.4 GB will be freed
Run with -apply to execute.
```

---

## Watch mode — process new files automatically

The `-watch` flag monitors the source directory and processes new files as they appear, using native filesystem events (inotify on Linux, FSEvents on macOS) for near-instant detection.

A periodic fallback scan (default: 60s) is also run to catch any files missed on network mounts (NFS/SMB) where kernel events may be unreliable.

```bash
# Watch with dry-run (default)
./rename-switch -watch -src /games

# Watch + apply + cleanup
./rename-switch -watch -apply -cleanup -src /games

# Custom fallback scan interval
./rename-switch -watch -watch-interval 120s -apply

# With Docker
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch -watch -apply -cleanup
```

Stop with `Ctrl+C`.

---

## How it works

**Fast path** — if the filename already contains a TitleID (`[0100XXXXXXXXXXXX]`), it's extracted directly (no nstool needed).

**Slow path** — for files without a TitleID, `nstool` reads the CNMT metadata from the file's internal structure to extract the TitleID and version. This handles both NSP and XCI formats, including SuperXCI (multiple games bundled) and newer key generations (keygen ≥ 18).

**Bundle detection** — for BASE files, all CNMT entries are scanned to detect bundled update patches and DLC content. The results are reflected in the filename suffix (`[+UPD vX][+N DLC]`).

---

## Switch keys

To decrypt files with `nstool`, place your Switch keys at:
```
~/.switch/prod.keys
~/.switch/title.keys
```

Files without keys in the filename can still be processed via the slow path, but encrypted content may not be readable without valid keys.

---

## Options

```
-apply                Apply renames (default: dry-run)
-cleanup              Remove redundant UPD/BASE files after renaming
-watch                Watch source directory and process new files automatically
-watch-interval DUR   Fallback scan interval for -watch mode (default: 60s)
-update-db            Refresh titledb cache from blawar/titledb
-src DIR              Source directory (default: current directory)
-dest DIR             Destination directory for renamed files (default: same as source)
-recursive            Scan subdirectories recursively
-nstool PATH          Path to nstool binary
-version              Show version
-h, -help             Show help
```

---

## Building from source

Requires Go 1.22+:

```bash
go build -o rename-switch .
```
