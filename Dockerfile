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
ARG TARGETOS TARGETARCH
COPY deploy/fetch-tools.sh /tmp/fetch-tools.sh
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
 && /tmp/fetch-tools.sh "$TARGETOS" "$TARGETARCH" /usr/local/bin \
 && apt-get purge -y curl xz-utils && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/* /tmp/fetch-tools.sh
COPY deploy/ffmpeg-NOTICE /usr/share/licenses/jellyfin-ffmpeg/NOTICE
COPY --from=build /out/photon-server /usr/local/bin/photon-server
COPY --from=web /web/build /usr/local/share/photon-server/web
COPY LICENSE /usr/share/licenses/photon-server/LICENSE
COPY deploy/entrypoint.sh /usr/local/bin/photon-entrypoint
# The server keeps its cache in the user's cache folder and its dumps in its config folder.
ENV XDG_CACHE_HOME=/var/cache XDG_CONFIG_HOME=/var/lib
RUN install -d -o 10001 -g 10001 /var/cache/photon-server /var/lib/photon-server/backups
EXPOSE 8640
ENTRYPOINT ["photon-entrypoint"]
