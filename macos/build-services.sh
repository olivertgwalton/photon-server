#!/bin/sh
# build-services.sh OUT builds the PostgreSQL and Valkey the Mac app runs for itself into
# OUT/postgres and OUT/valkey, for Apple silicon, from source pinned by checksum. PostgreSQL is
# built with Homebrew's icu4c@78, for the schema's ICU collation, whose libraries it carries. It
# finds its share and lib folders from where its binaries are, so every library is named relative
# to them too and the whole runs from inside the app.
set -eu

POSTGRES_VERSION=18.6
POSTGRES_SHA256=555610c24d53e4316da5b7d3fc25c279d96856d5e0e23ee308c328c5fa881d9f
VALKEY_VERSION=9.1.2
VALKEY_SHA256=19c23908e7d57e8d91ef85b41f5646307582f10f4f0fb999bbf89ed24ec9c983

out=$(mkdir -p "$1" && cd "$1" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
jobs=$(sysctl -n hw.ncpu)
fetch() { # URL SHA256 FILE
  curl -fsSL --retry 5 -o "$3" "$1"
  echo "$2  $3" | shasum -a 256 -c - >/dev/null
}

fetch "https://ftp.postgresql.org/pub/source/v$POSTGRES_VERSION/postgresql-$POSTGRES_VERSION.tar.bz2" \
  "$POSTGRES_SHA256" "$work/postgres.tar.bz2"
tar -xjf "$work/postgres.tar.bz2" -C "$work"
(
  cd "$work/postgresql-$POSTGRES_VERSION"
  # Only a socket in the user's own folder reaches it, so it has no TLS.
  PKG_CONFIG_PATH="$(brew --prefix icu4c@78)/lib/pkgconfig" \
    ./configure --prefix="$out/postgres" --with-icu --without-readline --without-openssl >/dev/null
  make -j"$jobs" -s world-bin >/dev/null
  make -s install-world-bin >/dev/null
)
# Each binary and library names libpq, ICU and their fellows where they were built; copied into
# lib and named from where they are instead, they load wherever the app is. A library copied in
# names others in turn, so the pass runs until none is copied.
lib="$out/postgres/lib"
copied=1
while [ "$copied" = 1 ]; do
  copied=0
  for f in "$out"/postgres/bin/* "$lib"/*.dylib "$lib"/postgresql/*.dylib; do
    [ -L "$f" ] && continue
    file "$f" | grep -q Mach-O || continue
    case "$f" in
      */bin/*) rel=@executable_path/../lib ;;
      */lib/postgresql/*) rel=@loader_path/.. ;;
      *) rel=@loader_path ;;
    esac
    case "$f" in "$lib"/*.dylib) chmod u+w "$f"; install_name_tool -id "@rpath/$(basename "$f")" "$f" ;; esac
    for dep in $(otool -L "$f" | awk 'NR > 1 {print $1}' | grep -v '^/usr/lib/\|^/System/\|^@'); do
      name=$(basename "$dep")
      if [ ! -e "$lib/$name" ]; then
        cp "$dep" "$lib/$name"
        # What it names beside itself, as ICU does its data, comes with it.
        for near in $(otool -L "$dep" | awk 'NR > 1 {print $1}' | grep '^@loader_path/'); do
          cp -n "$(dirname "$dep")/${near#@loader_path/}" "$lib/" || true
        done
        copied=1
      fi
      chmod u+w "$f"
      install_name_tool -change "$dep" "$rel/$name" "$f"
    done
  done
done
# Apple silicon runs nothing whose signature no longer matches it, as each relinked one's does
# not: each is signed again here, ad hoc, and the app's release signs them all with its own.
for f in "$out"/postgres/bin/* "$lib"/*.dylib; do
  [ -L "$f" ] && continue
  file "$f" | grep -q Mach-O && codesign --force --sign - "$f" 2>/dev/null
done
rm -rf "$out/postgres/include" "$out/postgres/lib/pkgconfig" "$out"/postgres/lib/*.a

fetch "https://github.com/valkey-io/valkey/archive/refs/tags/$VALKEY_VERSION.tar.gz" "$VALKEY_SHA256" "$work/valkey.tar.gz"
tar -xzf "$work/valkey.tar.gz" -C "$work"
(
  cd "$work/valkey-$VALKEY_VERSION"
  make -j"$jobs" -s BUILD_TLS=no valkey-server >/dev/null
  mkdir -p "$out/valkey/bin"
  cp src/valkey-server "$out/valkey/bin/"
)
