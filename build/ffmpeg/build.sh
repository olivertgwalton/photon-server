#!/usr/bin/env bash
# Builds ffmpeg and ffprobe for Linux, cross-compiled with Zig from macOS or Linux, and writes into
# OUT:
#   ffmpeg-<version>-<target>.tar.xz   ffmpeg, ffprobe, COPYING.GPLv3, BUILDCONF
#   ffmpeg-<version>-source.tar        the Corresponding Source: every archive in sources.txt and
#                                      this directory's scripts
#
# Every library is linked in statically; glibc (2.28, as Jellyfin's builds target) is the one
# shared dependency, so the host's GPU drivers, which VAAPI, NVENC and Quick Sync load at run
# time, still load. A finished library is not built again; delete .work/<target> to start over.
set -euo pipefail

target="${1:?usage: build.sh linux64|linuxarm64 OUT}"
out="$(mkdir -p "${2:?usage: build.sh linux64|linuxarm64 OUT}" && cd "$2" && pwd)"
here="$(cd "$(dirname "$0")" && pwd)"
case "$target" in
  linux64) arch=x86_64 ;;
  linuxarm64) arch=aarch64 ;;
  *) echo "unknown target $target" >&2; exit 2 ;;
esac
triple="$arch-linux-gnu"
work="$here/.work/$target"
downloads="$here/.work/downloads"
prefix="$work/prefix"
bin="$work/bin"
jobs="$(getconf _NPROCESSORS_ONLN)"
mkdir -p "$downloads" "$prefix" "$bin" "$work/src" "$work/done"

export PKG_CONFIG_LIBDIR="$prefix/lib/pkgconfig:$prefix/share/pkgconfig"
unset PKG_CONFIG_PATH
# The tools pinned in mise.toml go on PATH by their own folders: a shim resolves only inside the
# repository, and build systems run tools from temporary folders outside it.
if command -v mise >/dev/null; then
  eval "$(mise -C "$here" env -s bash)"
fi
# macOS's own gperf is too old for fontconfig; Homebrew's is keg-only.
export PATH="$bin:/opt/homebrew/opt/gperf/bin:$PATH"
# Zig refuses __DATE__ and __TIME__, which some libraries print in a banner; they are kept, and
# fixed to the FFmpeg release so a build is the same whenever it is run.
export SOURCE_DATE_EPOCH=1782864000
export CFLAGS="-O2 -fPIC -Wno-error=date-time" CXXFLAGS="-O2 -fPIC -Wno-error=date-time"

# Zig is the compiler and linker; build systems want them as single executables, by the names a
# GNU cross toolchain would have. They call Zig by its own path, as a version manager's shim may
# not resolve from the temporary folders build systems test compilers in.
zig="$(zig env | sed -n 's/.*zig_exe[" =:]*"\([^"]*\)".*/\1/p')"
tool() {
  printf '#!/bin/sh\nexec %s "$@"\n' "$2" >"$bin/$triple-$1"
  chmod +x "$bin/$triple-$1"
}
# Zig compiles rather than only preprocesses when given -E and -c together, as fontconfig gives
# them; GCC and Clang let -E win. And it refuses the glibc version in the target when reading
# standard input with no language named, as older Meson asks for the predefined macros.
compiler() {
  cat >"$bin/$triple-$1" <<EOF
#!/bin/sh
case " \$* " in *" -E "*) for a; do shift; [ "\$a" = -c ] || set -- "\$@" "\$a"; done ;; esac
case " \$* " in *" -x "*) ;; *" - "*) set -- -x $3 "\$@" ;; esac
exec $zig $2 -target $triple.2.28 "\$@"
EOF
  chmod +x "$bin/$triple-$1"
}
compiler cc cc c
compiler gcc cc c
compiler c++ c++ c++
compiler g++ c++ c++
tool ar "$zig ar"
tool ranlib "$zig ranlib"
tool nm nm
tool strings strings
tool strip true

cat >"$work/toolchain.cmake" <<EOF
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR $arch)
set(CMAKE_C_COMPILER $bin/$triple-cc)
set(CMAKE_CXX_COMPILER $bin/$triple-c++)
set(CMAKE_AR $bin/$triple-ar)
set(CMAKE_RANLIB $bin/$triple-ranlib)
set(CMAKE_FIND_ROOT_PATH $prefix)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_PACKAGE ONLY)
EOF
cat >"$work/cross.meson" <<EOF
[binaries]
c = '$bin/$triple-cc'
cpp = '$bin/$triple-c++'
ar = '$bin/$triple-ar'
ranlib = '$bin/$triple-ranlib'
strip = '$bin/$triple-strip'
pkg-config = 'pkg-config'
nasm = 'nasm'

[host_machine]
system = 'linux'
cpu_family = '$arch'
cpu = '$arch'
endian = 'little'

[built-in options]
default_library = 'static'
prefix = '$prefix'
libdir = 'lib'
b_staticpic = true
EOF

# fetch NAME unpacks NAME's pinned archive, checked against sources.sha256, and enters it.
fetch() {
  local name="$1" version url archive
  read -r _ version url < <(grep "^$name " "$here/sources.txt")
  archive="$downloads/$name-$version.tar.${url##*.tar.}"
  [[ -f "$archive" ]] || curl -fsSL -o "$archive" "$url"
  (cd "$downloads" && grep " $(basename "$archive")$" "$here/sources.sha256" | shasum -a 256 -c --quiet)
  rm -rf "$work/src/$name" && mkdir -p "$work/src/$name"
  tar -xf "$archive" -C "$work/src/$name" --strip-components 1
  cd "$work/src/$name"
}

cmake_build() {
  cmake -S . -B build -G Ninja -DCMAKE_TOOLCHAIN_FILE="$work/toolchain.cmake" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
    -DBUILD_SHARED_LIBS=OFF -DCMAKE_POSITION_INDEPENDENT_CODE=ON "$@"
  cmake --build build -j "$jobs"
  cmake --install build
}

meson_build() {
  meson setup build --cross-file "$work/cross.meson" --buildtype release "$@"
  meson compile -C build -j "$jobs"
  meson install -C build
}

configure_build() {
  ./configure --host="$triple" --prefix="$prefix" --enable-static --disable-shared \
    CC="$triple-cc" CXX="$triple-c++" AR="$triple-ar" RANLIB="$triple-ranlib" "$@"
  make -j "$jobs"
  make install
}

# build NAME runs build_NAME once.
build() {
  [[ -f "$work/done/$1" ]] && return
  echo "--- $1" >&2
  (fetch "$1" && "build_${1//-/_}")
  touch "$work/done/$1"
}

build_zlib() {
  CHOST="$triple" CC="$triple-cc" AR="$triple-ar" RANLIB="$triple-ranlib" ./configure --static --prefix="$prefix"
  make -j "$jobs" && make install
}
build_expat() {
  cmake_build -DEXPAT_BUILD_TOOLS=OFF -DEXPAT_BUILD_EXAMPLES=OFF -DEXPAT_BUILD_TESTS=OFF \
    -DEXPAT_BUILD_DOCS=OFF -DEXPAT_SHARED_LIBS=OFF
}
build_freetype() {
  meson_build -Dzlib=system -Dbzip2=disabled -Dpng=disabled -Dharfbuzz=disabled -Dbrotli=disabled
}
build_fribidi() { meson_build -Ddocs=false -Dbin=false -Dtests=false; }
build_harfbuzz() {
  meson_build -Dfreetype=enabled -Dglib=disabled -Dgobject=disabled -Dcairo=disabled -Dicu=disabled \
    -Dtests=disabled -Ddocs=disabled -Dutilities=disabled
}
build_fontconfig() {
  # Installed under DESTDIR so it still reads the host's /etc/fonts at run time.
  meson setup build --cross-file "$work/cross.meson" --buildtype release --sysconfdir=/etc \
    --localstatedir=/var -Ddoc=disabled -Dtests=disabled -Dtools=disabled -Dcache-build=disabled \
    -Dnls=disabled
  meson compile -C build -j "$jobs"
  DESTDIR="$work/fontconfig-root" meson install -C build
  cp -R "$work/fontconfig-root$prefix/." "$prefix/"
}
build_libass() { meson_build -Dfontconfig=enabled -Drequire-system-font-provider=false -Dtest=disabled; }
build_x264() {
  ./configure --host="$triple" --cross-prefix="$bin/$triple-" --prefix="$prefix" \
    --enable-static --enable-pic --disable-cli
  make -j "$jobs" && make install
}
build_dav1d() { meson_build -Denable_tools=false -Denable_tests=false; }
build_svt_av1() { cmake_build -DBUILD_APPS=OFF -DBUILD_TESTING=OFF; }
build_opus() { configure_build --disable-doc --disable-extra-programs; }
build_lame() { configure_build --disable-frontend --disable-gtktest; }
build_zimg() { autoreconf -fi && configure_build; }
build_libdrm() {
  local off=()
  for d in intel radeon amdgpu nouveau vmwgfx omap exynos freedreno tegra vc4 etnaviv; do
    off+=("-D$d=disabled")
  done
  meson_build -Dtests=false -Dman-pages=disabled -Dvalgrind=disabled -Dcairo-tests=disabled "${off[@]}"
}
build_libva() {
  # libva builds only shared libraries; static, as Jellyfin links it, ffmpeg starts on a host with
  # no libva at all. Drivers are the host's, where Debian and Ubuntu keep them, and
  # LIBVA_DRIVERS_PATH overrides that.
  sed -i.orig 's/shared_library(/library(/' va/meson.build
  meson_build -Ddriverdir="/usr/lib/$triple/dri" -Dwith_x11=no -Dwith_glx=no -Dwith_wayland=no \
    -Denable_docs=false
}
build_nv_codec_headers() { make PREFIX="$prefix" install; }
build_libvpl() {
  cmake_build -DBUILD_TESTS=OFF -DBUILD_EXAMPLES=OFF -DBUILD_TOOLS=OFF -DINSTALL_EXAMPLES=OFF
}
build_mbedtls() { cmake_build -DENABLE_PROGRAMS=OFF -DENABLE_TESTING=OFF; }
build_opencl_headers() { cmake_build -DBUILD_TESTING=OFF; }
build_opencl_icd_loader() { cmake_build -DBUILD_TESTING=OFF -DENABLE_OPENCL_LAYERINFO=OFF; }

libraries=(zlib expat freetype fribidi harfbuzz fontconfig libass x264 dav1d svt-av1 opus lame zimg
  libdrm libva nv-codec-headers mbedtls opencl-headers opencl-icd-loader)
features=(
  --enable-gpl --enable-version3
  --enable-zlib --enable-libfreetype --enable-libfribidi --enable-libharfbuzz --enable-libfontconfig
  --enable-libass --enable-libx264 --enable-libdav1d --enable-libsvtav1 --enable-libopus
  --enable-libmp3lame --enable-libzimg --enable-mbedtls
  --enable-libdrm --enable-vaapi --enable-ffnvcodec --enable-nvenc --enable-nvdec --enable-cuvid
  --enable-opencl
)
# Quick Sync is Intel's, and its runtime x86's alone, as in Jellyfin's builds.
if [[ "$arch" == x86_64 ]]; then
  libraries+=(libvpl)
  features+=(--enable-libvpl)
fi
for lib in "${libraries[@]}"; do
  build "$lib"
done

build_ffmpeg() {
  ./configure --prefix="$prefix" --enable-cross-compile --target-os=linux --arch="$arch" \
    --cross-prefix="$bin/$triple-" --cc="$triple-cc" --cxx="$triple-c++" \
    --pkg-config=pkg-config --pkg-config-flags=--static \
    --extra-cflags="-I$prefix/include" --extra-ldflags="-L$prefix/lib -s" \
    --extra-libs="-lc++ -lpthread -ldl -lm" \
    --disable-shared --enable-static --disable-debug --disable-doc --disable-ffplay --disable-stripping \
    "${features[@]}"
  # Zig's glibc 2.28 still exports sysctl, which configure checks for, but its headers are newer
  # than 2.32's and have no sys/sysctl.h.
  sed -i.orig 's/^#define HAVE_SYSCTL 1$/#define HAVE_SYSCTL 0/' config.h
  make -j "$jobs"
  make install
}
build ffmpeg

version="$(grep '^ffmpeg ' "$here/sources.txt" | cut -d' ' -f2)"
pkg="$work/pkg/ffmpeg-$version-$target"
rm -rf "$pkg" && mkdir -p "$pkg"
cp "$prefix/bin/ffmpeg" "$prefix/bin/ffprobe" "$pkg/"
cp "$work/src/ffmpeg/COPYING.GPLv3" "$pkg/"
printf '%s\n' "${features[@]}" >"$pkg/BUILDCONF"
tar -C "$work/pkg" -cJf "$out/ffmpeg-$version-$target.tar.xz" "ffmpeg-$version-$target"

src="$here/.work/source/ffmpeg-$version-source"
rm -rf "$src" && mkdir -p "$src/sources"
cp "$downloads"/*.tar.* "$src/sources/"
cp "$here/build.sh" "$here/sources.txt" "$here/sources.sha256" "$here/SOURCE.md" "$src/"
tar -C "$here/.work/source" -cf "$out/ffmpeg-$version-source.tar" "ffmpeg-$version-source"
