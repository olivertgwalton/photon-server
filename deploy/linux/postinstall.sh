#!/bin/sh
# The server runs as a user of its own, in the groups that own the GPUs' render nodes.
set -e
if ! getent passwd photon-server >/dev/null; then
  useradd --system --user-group --home-dir /var/lib/photon-server --no-create-home \
    --shell /usr/sbin/nologin photon-server
fi
for group in render video; do
  if getent group "$group" >/dev/null; then
    usermod -aG "$group" photon-server
  fi
done
chown root:photon-server /etc/photon-server/env
chmod 640 /etc/photon-server/env
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable photon-server.service >/dev/null
  systemctl try-restart photon-server.service
fi
