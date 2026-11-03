#!/bin/sh
# Run as root on Debian/Ubuntu. Does not edit SSH password-authentication settings.
set -eu
[ "$(id -u)" = 0 ] || { echo 'Root required.' >&2; exit 1; }
linad_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
linad_start=yes
linad_pam=yes
linad_server=
linad_device=
linad_ca=
linad_http=no
while [ "$#" -gt 0 ]; do
 case "$1" in
  --server) linad_server=$2; shift 2;;
  --device-id) linad_device=$2; shift 2;;
  --ca-file) linad_ca=$2; shift 2;;
  --allow-http) linad_http=yes; shift;;
  --no-start) linad_start=no; shift;;
  --no-pam) linad_pam=no; shift;;
  *) echo "Unknown option: $1" >&2; exit 1;;
 esac
done
[ -f /etc/debian_version ] || { echo 'Only Debian/Ubuntu are supported by this installer.' >&2; exit 1; }
for linad_command in useradd usermod groupadd pam-auth-update; do command -v "$linad_command" >/dev/null || { echo "Missing prerequisite: $linad_command" >&2; exit 1; }; done
if [ "$linad_start" = yes ]; then command -v systemctl >/dev/null; [ -d /run/systemd/system ] || { echo 'systemd is not running; use --no-start for image provisioning.' >&2; exit 1; }; fi
case "$(uname -m)" in x86_64) linad_arch=amd64;; aarch64|arm64) linad_arch=arm64;; *) echo 'Unsupported architecture.' >&2; exit 1;; esac
install -d -o root -g root -m 0700 /etc/linad-linux /var/lib/linad-linux
install -d -o root -g root -m 0755 /usr/local/libexec
install -o root -g root -m 0755 "$linad_root/dist/linad-linux-$linad_arch" /usr/local/libexec/linad-linux.new
mv -f /usr/local/libexec/linad-linux.new /usr/local/libexec/linad-linux
if [ ! -e /etc/linad-linux/client.json ]; then
 [ -n "$linad_server" ] && [ -n "$linad_device" ] || { echo '--server and --device-id required for initial enrollment.' >&2; exit 1; }
 set -- enroll --server "$linad_server" --device-id "$linad_device"
 if [ -n "$linad_ca" ]; then install -o root -g root -m 0600 "$linad_ca" /etc/linad-linux/ca.pem; set -- "$@" --ca-file /etc/linad-linux/ca.pem; fi
 if [ "$linad_http" = yes ]; then set -- "$@" --allow-http; fi
 /usr/local/libexec/linad-linux "$@"
fi
# Successful sync is required before modifying any active PAM configuration.
/usr/local/libexec/linad-linux sync
if [ -f /usr/share/pam-configs/linad ] && ! cmp -s /usr/share/pam-configs/linad "$linad_root/packaging/linux/linad.pam-profile"; then echo 'Existing PAM profile differs; review it before replacing.' >&2; exit 1; fi
install -o root -g root -m 0644 "$linad_root/packaging/linux/linad.pam-service" /etc/pam.d/linad
if [ "$linad_pam" = yes ]; then
 install -o root -g root -m 0644 "$linad_root/packaging/linux/linad.pam-profile" /usr/share/pam-configs/linad
 linad_backup=$(mktemp -d /var/lib/linad-linux/pam-backup-XXXXXXXX)
 cp -a /etc/pam.d/common-* "$linad_backup/"
 DEBIAN_FRONTEND=noninteractive pam-auth-update --package --enable linad
 if ! grep -qF '/usr/local/libexec/linad-linux pam-auth' /etc/pam.d/common-auth || ! grep -qF '/usr/local/libexec/linad-linux pam-account' /etc/pam.d/common-account; then
  echo "PAM activation was not applied (custom PAM configuration). Original files are in $linad_backup. No forced overwrite was performed." >&2; exit 1
 fi
fi
install -d -m 0755 /etc/systemd/system
install -o root -g root -m 0644 "$linad_root/packaging/linux/linad-linux.service" /etc/systemd/system/linad-linux.service
if [ "$linad_start" = yes ]; then systemctl daemon-reload; systemctl enable linad-linux.service; systemctl restart linad-linux.service; fi
echo 'linad installed. Authorized Linux login names are visible in the admin user list. Local accounts remain available.'
