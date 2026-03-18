# ── Stage 1: Build Go binary ────────────────────────────────────────────────────
FROM golang:1.22-alpine AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o rename-switch .

# ── Stage 2: Compile nstool from source ─────────────────────────────────────────
FROM debian:bookworm-slim AS nstool-builder

RUN apt-get update && apt-get install -y \
    git \
    build-essential \
    ca-certificates \
    --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --recurse-submodules https://github.com/jakcron/nstool.git /nstool

WORKDIR /nstool
RUN make deps && make -j$(nproc)

# ── Stage 3: Runtime ────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder  /src/rename-switch       /usr/local/bin/rename-switch
COPY --from=nstool-builder /nstool/bin/nstool      /usr/local/bin/nstool

WORKDIR /games

ENTRYPOINT ["rename-switch", \
    "-src",    "/games", \
    "-nstool", "/usr/local/bin/nstool"]
