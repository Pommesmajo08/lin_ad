#!/bin/sh
# Keep accounts, homes and root-only credentials for recovery; do not remove user data.
set -eu
[ "$(id -u)" = 0 ] || { echo 'Root required.' >&2; exit 1; }
if [ -d /run/systemd/system ]; then systemctl disable --now linad-linux.service; fi
/usr/local/libexec/linad-linux deactivate
DEBIAN_FRONTEND=noninteractive pam-auth-update --package --disable linad
if grep -qF /usr/local/libexec/linad-linux /etc/pam.d/common-auth /etc/pam.d/common-account; then echo 'PAM still references linad; inspect local modifications before removal.' >&2; exit 1; fi
rm -f /usr/share/pam-configs/linad /etc/pam.d/linad /etc/systemd/system/linad-linux.service
if [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
echo 'PAM integration removed. Managed accounts are expired; home directories, registry and credentials were retained.'
