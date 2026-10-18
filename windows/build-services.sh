#!/bin/sh
# build-services.sh OUT puts the PostgreSQL and Valkey Photon for Windows runs for itself in
# OUT/postgres and OUT/valkey. PostgreSQL is EDB's build, the one Windows has, pinned by checksum
# and cut to what initdb, the server, and photon's backups use. Valkey has no Windows build: it is
# built from the source macos/build-services.sh pins, under MSYS2's POSIX layer, whose DLL it
# carries. Run it in an MSYS2 shell with gcc, make, curl and unzip.
set -eu

POSTGRES_VERSION=18.6-5
POSTGRES_SHA256=e2246ba91d22345bc3d017586c09ede52d9df180b1eeb480f050445f1cad84e2
VALKEY_VERSION=9.1.2
VALKEY_SHA256=19c23908e7d57e8d91ef85b41f5646307582f10f4f0fb999bbf89ed24ec9c983

out=$(mkdir -p "$1" && cd "$1" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fetch() { # URL SHA256 FILE
  curl -fsSL --retry 5 -o "$3" "$1"
  echo "$2  $3" | sha256sum -c - >/dev/null
}

fetch "https://get.enterprisedb.com/postgresql/postgresql-$POSTGRES_VERSION-windows-x64-binaries.zip" \
  "$POSTGRES_SHA256" "$work/postgres.zip"
# Whole, as MSYS2's unzip matches no folder below a pattern's.
unzip -q "$work/postgres.zip" -d "$work"
pg="$out/postgres"
rm -rf "$pg"
mkdir -p "$pg/bin" "$pg/share/licenses"
for tool in postgres initdb pg_ctl psql pg_dump pg_restore; do
  cp "$work/pgsql/bin/$tool.exe" "$pg/bin/"
done
# Every library beside them but pgAdmin's and StackBuilder's widgets.
find "$work/pgsql/bin" -name '*.dll' ! -name 'wx*' -exec cp {} "$pg/bin/" \;
# EDB's build needs the Visual C++ runtime, which a Windows install may lack; Microsoft lets an
# app carry it beside itself.
for dll in vcruntime140 vcruntime140_1 msvcp140; do
  cp "/c/Windows/System32/$dll.dll" "$pg/bin/"
done
mkdir "$pg/lib"
find "$work/pgsql/lib" -maxdepth 1 -name '*.dll' ! -name '*pl*perl*' ! -name '*plpython*' ! -name 'pltcl*' \
  -exec cp {} "$pg/lib/" \;
cp -R "$work/pgsql/share/." "$pg/share/"
rm -rf "$pg/share/doc"
cp "$work/pgsql/server_license.txt" "$work/pgsql/commandlinetools_3rd_party_licenses.txt" "$pg/share/licenses/"

fetch "https://github.com/valkey-io/valkey/archive/refs/tags/$VALKEY_VERSION.tar.gz" "$VALKEY_SHA256" "$work/valkey.tar.gz"
tar -xzf "$work/valkey.tar.gz" -C "$work"
(
  cd "$work/valkey-$VALKEY_VERSION"
  # Valkey asks for GNU extensions on Linux alone; MSYS2 hides dladdr, which it uses, without them.
  make -j"$(nproc)" -s BUILD_TLS=no SERVER_CFLAGS=-D_GNU_SOURCE valkey-server >/dev/null
)
vk="$out/valkey"
rm -rf "$vk"
mkdir -p "$vk/bin" "$vk/share/licenses"
cp "$work/valkey-$VALKEY_VERSION/src/valkey-server.exe" "$vk/bin/"
cp "$work/valkey-$VALKEY_VERSION/COPYING" "$vk/share/licenses/valkey-COPYING"
# MSYS2's runtime and whatever else of its it links, from its own bin, each with the licence of
# the package it comes from.
for dll in $(ldd "$vk/bin/valkey-server.exe" | awk '$3 ~ "^/usr/bin/" {print $3}'); do
  cp "$dll" "$vk/bin/"
  pkg=$(pacman -Qqo "$dll")
  licences=$(pacman -Qlq "$pkg" | grep -iE '^/usr/share/licenses/.+[^/]$|/(copying|licen[cs]e)[^/]*$' || true)
  if [ -z "$licences" ]; then
    echo "no licence found in $pkg, which $dll comes from:" >&2
    pacman -Qlq "$pkg" >&2
    exit 1
  fi
  for f in $licences; do
    cp "$f" "$vk/share/licenses/$pkg-$(basename "$f")"
  done
done
