#!/bin/sh
# All account/PAM changes happen inside a disposable container, never on the host.
set -eu
cd "$(dirname "$0")/.."
linad_test_image=${LINAD_TEST_IMAGE:-linad-pam-test:local}
set -- docker build -f tests/linux/Dockerfile -t "$linad_test_image" --build-arg "BASE_IMAGE=${LINAD_TEST_BASE_IMAGE:-debian:bookworm-slim}" --build-arg "SUDO_PACKAGE=${LINAD_TEST_SUDO_PACKAGE:-sudo}"
# Optional managed-cloud trust bundle. It is a build secret, not an image layer.
if [ -n "${LINAD_BUILD_CA_FILE:-}" ]; then set -- "$@" --secret "id=proxy_ca,src=$LINAD_BUILD_CA_FILE"; fi
"$@" tests/linux
docker run --rm --mount "type=bind,src=$(pwd),dst=/src,readonly" \
  -e LINAD_ISOLATED_PAM_TEST=1 "$linad_test_image" python3 /src/tests/linux/integration.py
