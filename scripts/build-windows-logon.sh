#!/bin/sh
# Optional Linux cross-build. Native Windows release builds use the real SDK.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p dist
if command -v x86_64-w64-mingw32-g++ >/dev/null 2>&1; then
 x86_64-w64-mingw32-g++ -std=c++17 -O2 -Wall -Wextra -Werror -DUNICODE -D_UNICODE -shared -static native/windows-logon/provider.cpp -o dist/linad-credential-provider.dll -lole32 -luuid -lsecur32 -ladvapi32
 x86_64-w64-mingw32-g++ -std=c++17 -O2 -Wall -Wextra -Werror -DUNICODE -D_UNICODE -static -municode native/windows-logon/probe.cpp -o dist/linad-logon-probe.exe -lole32 -luuid -lsecur32 -ladvapi32 -luserenv
else
 command -v docker >/dev/null || { echo 'MinGW oder Docker erforderlich; alternativ auf Windows scripts/build-windows-logon.ps1 verwenden.' >&2; exit 1; }
 set -- docker build -f tests/windows/Dockerfile.compiler -t linad-windows-compiler:local
 if [ -n "${LINAD_BUILD_CA_FILE:-}" ]; then set -- "$@" --secret "id=proxy_ca,src=$LINAD_BUILD_CA_FILE"; fi
 "$@" tests/windows
 docker run --rm --user "$(id -u):$(id -g)" --mount "type=bind,src=$root,dst=/src" --workdir /src linad-windows-compiler:local sh scripts/build-windows-logon.sh
fi
