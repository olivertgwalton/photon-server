#!/bin/sh
set -e
# Only on removal: an upgrade restarts the server in postinstall.
case "$1" in remove|0)
  if [ -d /run/systemd/system ]; then
    systemctl disable --now photon-server.service || true
  fi ;;
esac
