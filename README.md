# rename-switch

Automatically rename Nintendo Switch game files (`.nsp`, `.xci`, `.nsz`, `.xcz`) using official titles from [blawar/titledb](https://github.com/blawar/titledb). Bundled updates and DLC are detected and reflected directly in the filename.

```
# Before
0100A3900C3E8000.nsp
base_game.xci
v196608.nsp

# After
Fire Emblem™ - Three Houses [BASE][010055d009f78000][v0][+UPD v196608][+6 DLC].xci
Fire Emblem™ - Three Houses [UPD][010055d009f78000][v196608].nsp
```

## Features

- **Auto-rename** — resolves official game titles from titledb by TitleID
- **Bundle detection** — detects updates and DLC bundled inside a BASE dump and appends `[+UPD vX][+N DLC]` to the filename
- **Cleanup** — identifies redundant files (superseded UPDs, duplicate BASEs) and shows the total space that can be freed
- **Watch mode** — monitors a directory and renames new files as they arrive
- **Safe by default** — dry-run mode shows all changes before anything is modified; use `-apply` to execute
- **Docker ready** — zero-install option, works on any OS

---

## Requirements

- **Switch keys** — `~/.switch/prod.keys` and `~/.switch/title.keys` (needed to read files that don't already have a TitleID in their filename — see [Switch keys](#switch-keys))
- **Docker** (recommended) OR **Go 1.22+** + [nstool](https://github.com/jakcron/nstool)

---

## Quickstart

### With Docker (recommended)

[Install Docker](https://docs.docker.com/get-started/get-docker/)

`~/.switch` must contain your Switch keys (`prod.keys`, `title.keys`) — see [Switch keys](#switch-keys). It also stores the titledb cache (auto-downloaded on first run).

```bash
# Preview all changes — rename, cleanup redundant files, flatten subdirectories
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch -src /games -dest /games -recursive -cleanup -prune-empty

# Apply
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  ghcr.io/naonak/rename-switch -src /games -dest /games -recursive -cleanup -prune-empty -apply
```

### Without Docker

Place your Switch keys in `~/.switch/prod.keys` and `~/.switch/title.keys` — see [Switch keys](#switch-keys). The titledb cache is stored there too (auto-downloaded on first run).

```bash
# Clone and build
git clone https://github.com/naonak/rename-switch.git
cd rename-switch
go build -o rename-switch .

# Preview all changes
./rename-switch -src /games -dest /games -recursive -cleanup -prune-empty

# Apply
./rename-switch -src /games -dest /games -recursive -cleanup -prune-empty -apply
```

> **Note:** `-dest` equal to `-src` renames files in place at the root of the directory. Files in subdirectories are moved to the root; `-prune-empty` then removes the now-empty subdirectories.

---

## Filename format

```
Game Name [TYPE][titleid][vVERSION].ext
Game Name [BASE][titleid][vVERSION][+UPD vX][+N DLC].ext
```

Examples:
```
Metroid Prime Remastered [BASE][010012101468c000][v0].xci
Fire Emblem™ - Three Houses [BASE][010055d009f78000][v0][+UPD v196608][+6 DLC].xci
Blazing Beaks [UPD][010021a00de54800][v655360].nsp
```

| Type | Meaning |
|------|---------|
| `BASE` | Base game (TitleID ends in `000`) |
| `UPD` | Update / patch (TitleID ends in `800`) |
| `DLC` | Downloadable content |

---

## Features in depth

### Cleanup (`-cleanup`)

Identifies and optionally removes redundant files:

- **Standalone UPD covered by a bundled BASE** — if you have both `Game [BASE][...][+UPD v2]` and `Game [UPD][...][v2]`, the standalone UPD is redundant
- **Outdated UPD files** — older update versions superseded by a newer one
- **Duplicate BASE files** — the most complete version is kept (highest bundled update + most DLCs); a BASE with unique DLCs is always kept

Each file marked for deletion shows its size; the total space freed is shown at the end.

```bash
# Preview what would be deleted (dry-run)
./rename-switch -cleanup

# Apply deletions
./rename-switch -apply -cleanup
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

### Watch mode (`-watch`)

Monitors the source directory and renames new files as they appear.

A periodic fallback scan (default: 60s) catches files missed on network mounts (NFS/SMB) where filesystem events may be unreliable.

```bash
# Watch + apply + cleanup
./rename-switch -watch -apply -cleanup -src /games

# Custom fallback scan interval
./rename-switch -watch -watch-interval 120s -apply -src /games
```

Stop with `Ctrl+C`.

#### Docker Compose

```yaml
services:
  rename-switch:
    image: ghcr.io/naonak/rename-switch:latest
    restart: unless-stopped
    volumes:
      - ~/.switch:/root/.switch
      - /path/to/switch-games:/games
    command: ["-watch", "-apply", "-cleanup"]
```

---

### Destination directory (`-dest`)

By default, files are renamed **in place**. Use `-dest` to move renamed files to a separate directory (created automatically if needed).

| Scenario | Behavior |
|----------|----------|
| `-dest` not set | Files renamed in place, no move |
| `-dest` same as `-src` | Root-level files renamed in place; subdirectory files (with `-recursive`) moved to the root |
| `-dest` different from `-src` | Files moved and renamed into `-dest` |

```bash
# Rename in place
./rename-switch -src /games -apply

# Move renamed files to a separate directory
./rename-switch -src /games -dest /games-renamed -apply

# Flatten subdirectories into dest root
./rename-switch -src /games -dest /games-renamed -recursive -apply
```

> **Tip:** combine with `-prune-empty` to remove subdirectories left empty in `-src` after the move.

---

### Prune empty directories (`-prune-empty`)

Removes subdirectories left empty after renaming or moving files. Works bottom-up so nested empty directories are caught as well. The source root is never removed.

```bash
./rename-switch -src /games -dest /games-out -apply -prune-empty
```

Example output:
```
=== PRUNE EMPTY DIRS (dry run) ===
  [RMDIR] /games/Old Subfolder
  [RMDIR] /games/Another Empty Dir

2 empty director(y/ies) to remove
Run with -apply to execute.
```

---

## How it works

**Fast path** — if the filename already contains a TitleID (`[0100XXXXXXXXXXXX]`), it's extracted directly. No nstool required.

**Slow path** — for files without a TitleID, `nstool` reads the CNMT metadata from the file's internal structure to extract the TitleID and version. Handles NSP and XCI formats, including SuperXCI and newer key generations (keygen ≥ 18).

**Bundle detection** — for BASE files, all CNMT entries are scanned to detect bundled update patches and DLC. Results are reflected in the filename suffix (`[+UPD vX][+N DLC]`).

---

## Switch keys

Required to decrypt files via the slow path. Place your keys at:

```
~/.switch/prod.keys
~/.switch/title.keys
```

With Docker, mount `~/.switch` as a volume:

```bash
-v ~/.switch:/root/.switch
```

Files that already have a TitleID in their filename are processed via the fast path and don't need keys.

---

## All options

```
-apply                Apply renames (default: dry-run)
-cleanup              Remove redundant UPD/BASE files after renaming
-prune-empty          Remove empty directories left after renaming or moving files
-watch                Watch source directory and process new files automatically
-watch-interval DUR   Fallback scan interval for -watch mode (default: 60s)
-update-db            Force refresh titledb cache (auto-downloaded on first run)
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

Or with Docker:

```bash
docker build -t rename-switch .
docker run --rm \
  -v ~/.switch:/root/.switch \
  -v /path/to/switch-games:/games \
  rename-switch -apply
```
