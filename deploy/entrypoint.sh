#!/bin/sh
# Started as root, the server drops to its own user in the groups owning the GPU devices passed in,
# as Plex's image does, so no host's render group id need be looked up. Started as another user
# (compose's user:, Kubernetes' runAsUser), it runs as that user.
set -eu
if [ "$(id -u)" != 0 ]; then
  exec photon-server "$@"
fi
# The devices alone, not /dev/dri/by-path, and never root's group, which would open what it owns.
groups=$(stat -c %g /dev/dri/card* /dev/dri/renderD* 2>/dev/null | grep -vx 0 | sort -un | paste -sd, -)
if [ -n "$groups" ]; then
  set -- --groups="$groups" photon-server "$@"
else
  set -- --clear-groups photon-server "$@"
fi
exec setpriv --reuid=10001 --regid=10001 --inh-caps=-all --bounding-set=-all --no-new-privs "$@"
