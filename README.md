# rename-switch

A CLI tool to rename Nintendo Switch game files (`.nsp`, `.xci`, `.nsz`, `.xcz`) with standardized names using official titles from [blawar/titledb](https://github.com/blawar/titledb) and metadata from [nstool](https://github.com/jakcron/nstool).

## Output format

```
Game Name [TYPE][titleid][vVERSION].ext
```

Examples:
```
Metroid Prime Remastered [BASE][010012100d6e0000][v0].xci
Fire Emblem - Three Houses [BASE][010055d009f78000][v0].nsp
Blazing Beaks [UPD][010021a00de54800][v655360].nsp
```

Types: `BASE` (base game), `UPD` (update/patch), `DLC` (downloadable content)

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

## How it works

**Fast path** — if the filename already contains a TitleID (`[0100XXXXXXXXXXXX]`), it's extracted directly (no nstool needed).

**Slow path** — for files without a TitleID, `nstool` reads the CNMT metadata from the file's internal structure to extract the TitleID and version. This handles both NSP and XCI formats, including SuperXCI (multiple games bundled) and newer key generations (keygen ≥ 18).

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
-apply          Apply renames (default: dry-run)
-update-db      Refresh titledb cache from blawar/titledb
-src DIR      Games directory (default: current directory)
-nstool PATH    Path to nstool binary
-version        Show version
-h, -help       Show help
```

---

## Building from source

Requires Go 1.22+:

```bash
go build -o rename-switch .
```

No external Go dependencies — uses stdlib only.
