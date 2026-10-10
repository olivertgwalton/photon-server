#!/bin/bash
# photon starts the server beside the PostgreSQL and Valkey the Flatpak carries, each reached only
# through a socket in the app's own runtime folder, so neither needs a password or a port. It makes
# the database the first time, migrates it, opens the web app, starts the server again when it
# exits 75 after a restore, and stops all three when the server ends or photon is told to stop.
set -eu

data="$XDG_DATA_HOME/photon"
run="$XDG_RUNTIME_DIR/app/$FLATPAK_ID"
logs="$data/logs"
mkdir -p "$data" "$run" "$logs"
pgdata="$data/postgres"

if [ ! -f "$pgdata/PG_VERSION" ]; then
  initdb -D "$pgdata" -U photon --auth=trust -E UTF8 --locale=C >>"$logs/postgres.log"
  printf "listen_addresses = ''\nunix_socket_directories = '%s'\n" "$run" >>"$pgdata/postgresql.conf"
fi
# The data folder is the PostgreSQL major's that made it: another's is refused, not started.
made=$(cat "$pgdata/PG_VERSION")
if [ "$made" != 18 ]; then
  echo "photon: the database in $pgdata was made by PostgreSQL $made, not 18" >&2
  exit 1
fi

valkey=
stop() {
  [ -n "$valkey" ] && kill "$valkey" 2>/dev/null && wait "$valkey" 2>/dev/null
  pg_ctl -D "$pgdata" -m fast -w stop >>"$logs/postgres.log" 2>&1 || true
}
trap stop EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

pg_ctl -D "$pgdata" -l "$logs/postgres.log" -w start >/dev/null
if [ -z "$(psql -h "$run" -U photon -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname = 'photon'")" ]; then
  psql -h "$run" -U photon -d postgres -qc 'CREATE DATABASE photon'
fi

# As the compose file's: nothing durable lives here, so snapshots only keep live sessions across a
# restart.
rm -f "$run/valkey.sock"
valkey-server --port 0 --unixsocket "$run/valkey.sock" --unixsocketperm 700 --dir "$data" \
  --appendonly no --save "300 1" --maxmemory 256mb --maxmemory-policy volatile-lru \
  --logfile "$logs/valkey.log" &
valkey=$!
for _ in $(seq 100); do
  [[ -S $run/valkey.sock ]] && break
  sleep 0.1
done

export PHOTON_DATABASE_URL="postgres://photon@/photon?host=$run"
export PHOTON_VALKEY_URL="unix://$run/valkey.sock"
photon-server migrate

# The web app is opened once the server listens.
(
  for _ in $(seq 60); do
    if (exec 3<>/dev/tcp/127.0.0.1/8640) 2>/dev/null; then
      xdg-open http://localhost:8640 >/dev/null 2>&1 || true
      exit
    fi
    sleep 1
  done
) &

while :; do
  photon-server &
  server=$!
  # Told to stop, the server is told twice, so it plays no stream to its end.
  trap 'kill -TERM "$server" 2>/dev/null; kill -TERM "$server" 2>/dev/null' INT TERM
  status=0
  wait "$server" || status=$?
  # A wait a signal broke off is waited out.
  while kill -0 "$server" 2>/dev/null; do
    wait "$server" || status=$?
  done
  [ "$status" = 75 ] || exit "$status"
done
