#!/bin/sh
# Launcher of the cerne MCPB bundle (F-0455): runs the binary for this
# machine's OS and architecture, next to this file, with the arguments it got.
# MCPB overrides the command per OS only (darwin, linux, win32), never per
# architecture, so darwin and linux land here and uname picks the binary;
# Windows starts its .exe directly. The client runs `/bin/sh cerne.sh`, so this
# file needs no execute bit, and the binary gets its bit back if the bundle's
# unpacker dropped it.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) arch=$(uname -m) ;;
esac
bin="$(dirname "$0")/cerne_${os}_${arch}"
if [ ! -f "$bin" ]; then
  echo "cerne: no binary for ${os}/${arch} in this bundle" >&2
  exit 1
fi
[ -x "$bin" ] || chmod +x "$bin" || exit 1
exec "$bin" "$@"
