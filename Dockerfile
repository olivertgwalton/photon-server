# syntax=docker/dockerfile:1

# The web app is files, the same on every platform, so it is built once on the builder's.
FROM --platform=$BUILDPLATFORM oven/bun:1.4 AS web
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web .
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
ARG TARGETOS TARGETARCH VERSION=(devel)
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/photon-server ./cmd/photon-server

FROM ubuntu:26.04
ARG TARGETARCH
# jellyfin-ffmpeg's portable builds, pinned by checksum.
ARG FFMPEG_VERSION=8.1.3-1
ARG FFMPEG_SHA256_amd64=b86dc023c64a5a9a410e6e3a938a970460214fae78f31c1553c9f88f1c289b6a
ARG FFMPEG_SHA256_arm64=7bc8e8d0986f7f4693f63e9728570f671f07b6e21c05d5465dd7ce461d1631dc
# yt-dlp's standalone builds, for themes from ThemerrDB's links, pinned by checksum.
ARG YTDLP_VERSION=2026.08.19
ARG YTDLP_SHA256_amd64=58162f9bfdc27458ea47bfcb311cf47028f17d8154a8bf7d689861d46399230a
ARG YTDLP_SHA256_arm64=b16e4dab368a816cd05d477d698a605a6ae87ccee1c8ffd38fa21d7254141fcc
# The GPU runtimes jellyfin-ffmpeg loads: VAAPI drivers for Intel and AMD, Vulkan for libplacebo
# (Mesa's, with a CPU device where there is no GPU), and on amd64 Quick Sync's runtime and Intel's
# OpenCL for tone mapping. NVIDIA's come from the host through its container toolkit. Fonts are
# for subtitles libass draws into a transcode, found through fontconfig, whose configuration maps
# the fonts styles name (Arial, Times New Roman) to Liberation's, which share their metrics. Its
# cache is made as the fonts are installed, as the server's user can write none. pg_dump backs the
# database up, and pg_restore and psql put it back; pg_dump must be no older than the server it dumps.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl xz-utils mesa-va-drivers libvulkan1 mesa-vulkan-drivers \
      fontconfig fonts-liberation fonts-noto-core fonts-noto-cjk postgresql-client-18 \
 && if [ "$TARGETARCH" = amd64 ]; then \
      apt-get install -y --no-install-recommends intel-media-va-driver-non-free libmfx-gen1.2 intel-opencl-icd; \
    fi \
 && case "$TARGETARCH" in \
      amd64) target=linux64 sum=$FFMPEG_SHA256_amd64 ytdlp=yt-dlp_linux ytdlp_sum=$YTDLP_SHA256_amd64 ;; \
      arm64) target=linuxarm64 sum=$FFMPEG_SHA256_arm64 ytdlp=yt-dlp_linux_aarch64 ytdlp_sum=$YTDLP_SHA256_arm64 ;; \
    esac \
 && curl -fsSL --retry 5 -o /tmp/ffmpeg.tar.xz \
      "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v${FFMPEG_VERSION}/jellyfin-ffmpeg_${FFMPEG_VERSION}_portable_${target}-gpl.tar.xz" \
 && echo "$sum  /tmp/ffmpeg.tar.xz" | sha256sum -c - \
 && tar -xJf /tmp/ffmpeg.tar.xz -C /usr/local/bin ffmpeg ffprobe \
 && rm /tmp/ffmpeg.tar.xz \
 && curl -fsSL --retry 5 -o /usr/local/bin/yt-dlp \
      "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${ytdlp}" \
 && echo "$ytdlp_sum  /usr/local/bin/yt-dlp" | sha256sum -c - \
 && chmod 755 /usr/local/bin/yt-dlp \
 && apt-get purge -y curl xz-utils && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*
COPY deploy/ffmpeg-NOTICE /usr/share/licenses/jellyfin-ffmpeg/NOTICE
COPY --from=build /out/photon-server /usr/local/bin/photon-server
COPY --from=web /web/build /usr/local/share/photon-server/web
COPY LICENSE /usr/share/licenses/photon-server/LICENSE
ENV PHOTON_CACHE_DIR=/var/cache/photon-server PHOTON_BACKUP_DIR=/var/lib/photon-server/backups
RUN install -d -o 10001 -g 10001 /var/cache/photon-server /var/lib/photon-server/backups
USER 10001:10001
EXPOSE 8640
ENTRYPOINT ["photon-server"]
