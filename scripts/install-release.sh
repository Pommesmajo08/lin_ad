#!/bin/sh
# Standalone bootstrapper: also distributed as install.sh on GitHub Releases.
set -eu
repo=https://github.com/Pommesmajo08/lin_ad
version=latest
component=${1:-}
help() {
 cat <<'HELP'
Linad aus GitHub Releases installieren
  sh install.sh client                       GUI + CLI, aktueller Benutzer
  sudo sh install.sh device                  Ubuntu/Debian-Geräteanbindung
  sudo sh install.sh server                  Docker-Server (Linux AMD64)
  sudo sh install.sh device --version v0.1.0 -- --server https://linad.example --device-id ID
  --skip-dependencies                        Keine fehlenden Systempakete installieren
  --help                                     Hilfe
Ohne Komponente erscheint eine Auswahl. Updates: denselben Aufruf erneut starten.
Parameter nach -- werden an den Paket-Installer weitergereicht. Enrollment bleibt interaktiv.
HELP
}
fail() { printf 'Fehler: %s\n' "$*" >&2; exit 1; }
case "$component" in --help|-h) help; exit 0;; '')
 [ -t 0 ] || fail 'Komponente angeben: client, device oder server.'
 printf 'Linad installieren: 1) GUI/CLI  2) Linux-Gerät  3) Docker-Server\nAuswahl: '
 IFS= read -r choice
 case "$choice" in 1) component=client;; 2) component=device;; 3) component=server;; *) fail 'Ungültige Auswahl.';; esac;;
 *) shift;;
esac
auth_token=${GH_TOKEN:-${GITHUB_TOKEN:-}}
unset GH_TOKEN GITHUB_TOKEN
case "$auth_token" in *[!a-zA-Z0-9_]*) fail 'Ungültiges Format des GitHub-Tokens.';; esac
skip_dependencies=no
while [ "$#" -gt 0 ]; do
 case "$1" in
  --version) [ "$#" -ge 2 ] || fail 'Version fehlt.'; version=$2; shift 2;;
  --skip-dependencies) skip_dependencies=yes; shift;;
  --help|-h) help; exit 0;;
  --) shift; break;;
  *) break;;
 esac
done
# A delegated preflight must not install system dependencies.
check_only=no
for flag in "$@"; do if [ "$flag" = --check ]; then skip_dependencies=yes; check_only=yes; fi; done
case "$version" in ''|*[!a-zA-Z0-9._-]*) fail 'Ungültiger Release-Tag.';; esac
case "$component" in client|device|server) ;; *) fail 'Komponente muss client, device oder server sein.';; esac
[ "$(uname -s)" = Linux ] || fail 'Für Windows install-windows.cmd verwenden.'
case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) fail 'Architektur nicht unterstützt.';; esac
if [ "$component" != device ] && [ "$arch" != amd64 ]; then fail 'Server und Desktop-Client sind derzeit nur für AMD64 verfügbar.'; fi
if [ "$component" != client ]; then [ "$(id -u)" = 0 ] || fail 'Gerät/Server bitte mit sudo starten.'; fi
# Only missing prerequisites are installed; existing packages are never upgraded here.
missing=
need() { command -v "$1" >/dev/null 2>&1 || missing="$missing $2"; }
need curl curl
need sha256sum coreutils
need tar tar
if [ -n "$auth_token" ]; then need python3 python3; fi
if [ "$component" = device ]; then
 [ -f /etc/debian_version ] || fail 'Geräteanbindung unterstützt Debian/Ubuntu.'
 need useradd passwd; need pam-auth-update libpam-runtime; need sudo sudo; need flock util-linux
fi
if [ "$component" = server ]; then need openssl openssl; need flock util-linux; need docker docker.io; fi
if [ -n "$missing" ]; then
 [ "$skip_dependencies" = no ] || fail "Voraussetzungen fehlen:$missing"
 [ "$(id -u)" = 0 ] && command -v apt-get >/dev/null 2>&1 || fail "Bitte Voraussetzungen installieren:$missing. GUI-Installer danach ohne sudo starten."
 [ -z "$(dpkg --audit 2>&1)" ] || fail 'dpkg wurde unterbrochen. Zuerst sudo dpkg --configure -a ausführen.'
 printf 'Fehlende Voraussetzungen installieren:%s\n' "$missing"
 apt-get update
 # Package names are fixed above, not derived from remote data or user input.
 apt-get install -y --no-install-recommends $missing ca-certificates
fi
fetch() { curl --proto '=https' --proto-redir '=https' --fail --location --silent --show-error --retry 2 --connect-timeout 15 --max-time 900 "$@"; }
case "$component" in
 device) package=linad-linux-provisioner-$arch.tar.gz; installer=install-linux.sh;;
 server) package=linad-server-docker.tar.gz; installer=install-server.sh;;
 client) package=linad-client-linux-$arch.tar.gz; installer=install-client-linux.sh;;
esac
umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' 0
trap 'exit 130' INT
trap 'exit 143' TERM
# Tokens enter curl through stdin configuration, never argv, URLs or saved files.
api_fetch() {
 accept=$1; shift
 printf 'header = "Authorization: Bearer %s"\n' "$auth_token" | fetch --config - --header "Accept: $accept" "$@"
}
if [ -n "$auth_token" ]; then
 api=https://api.github.com/repos/Pommesmajo08/lin_ad/releases
 if [ "$version" = latest ]; then metadata=$api/latest; else metadata=$api/tags/$version; fi
 api_fetch application/vnd.github+json --output "$work/release.json" "$metadata" || fail 'Release nicht erreichbar. Token-Leserechte und Version prüfen.'
 python3 - "$work/release.json" "$package" > "$work/assets" <<'PYASSETS'
import json,re,sys
with open(sys.argv[1]) as source: release=json.load(source)
tag=release.get('tag_name','')
if not re.fullmatch(r'[a-zA-Z0-9._-]+',tag): raise SystemExit('Invalid release tag')
print(tag)
for name in ['SHA256SUMS',sys.argv[2]]:
    matches=[a['url'] for a in release.get('assets',[]) if a['name']==name]
    if len(matches)!=1 or not re.fullmatch(r'https://api\.github\.com/repos/Pommesmajo08/lin_ad/releases/assets/[0-9]+',matches[0]):
        raise SystemExit('Release asset missing or invalid: '+name)
    print(matches[0])
PYASSETS
 version=$(sed -n '1p' "$work/assets")
 sums_url=$(sed -n '2p' "$work/assets")
 package_url=$(sed -n '3p' "$work/assets")
 api_fetch application/octet-stream --output "$work/SHA256SUMS" "$sums_url" || fail 'Prüfsummen-Download fehlgeschlagen.'
 api_fetch application/octet-stream --output "$work/$package" "$package_url" || fail 'Paket-Download fehlgeschlagen.'
else
 if [ "$version" = latest ]; then
  resolved=$(fetch --head --output /dev/null --write-out '%{url_effective}' "$repo/releases/latest") || fail 'Kein öffentliches Release erreichbar. Für private Repositories GH_TOKEN mit Leserechten setzen.'
  case "$resolved" in "$repo/releases/tag/"*) version=${resolved##*/};; *) fail 'GitHub hat keinen eindeutigen Release-Tag geliefert.';; esac
 fi
 case "$version" in ''|*[!a-zA-Z0-9._-]*) fail 'Ungültiger Release-Tag.';; esac
 base=$repo/releases/download/$version
 fetch --output "$work/SHA256SUMS" "$base/SHA256SUMS" || fail 'Release-Prüfsummen fehlen oder sind nicht erreichbar.'
 fetch --output "$work/$package" "$base/$package" || fail 'Das passende Installationspaket fehlt im Release.'
fi
printf 'Linad %s · %s · %s\nDownloads prüfen …\n' "$version" "$component" "$arch"
expected=$(awk -v file="$package" '$2 == file && length($1) == 64 { print $1 }' "$work/SHA256SUMS")
case "$expected" in ''|*[!a-fA-F0-9]*) fail 'Keine eindeutige SHA-256-Prüfsumme für das Paket.';; esac
[ "${#expected}" = 64 ] || fail 'Mehrdeutige Prüfsumme.'
actual=$(sha256sum "$work/$package"); actual=${actual%% *}
[ "$expected" = "$actual" ] || fail 'Prüfsumme stimmt nicht überein. Installation wurde nicht gestartet.'
echo 'Prüfsumme stimmt. Paket entpacken …'
tar -xzf "$work/$package" -C "$work"
[ -f "$work/scripts/$installer" ] || fail 'Installationsskript fehlt im Paket.'
# Ubuntu provides Compose v2 in its standard repositories. Debian users can keep
# an existing Docker/Compose installation; never replace a working engine.
if [ "$component" = server ]; then
 if ! docker --host=unix:///var/run/docker.sock compose version >/dev/null 2>&1; then
  if [ "$skip_dependencies" = no ] && command -v apt-get >/dev/null 2>&1 && apt-cache show docker-compose-v2 >/dev/null 2>&1; then
   [ -z "$(dpkg --audit 2>&1)" ] || fail 'dpkg ist unvollständig; zuerst reparieren.'
   apt-get update
   apt-get install -y --no-install-recommends docker-compose-v2
  else
   fail 'Docker Compose v2 fehlt. Compose-Plugin installieren und denselben Aufruf erneut starten.'
  fi
 fi
 if [ "$check_only" = no ] && command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then systemctl start docker; fi
fi
unset GH_TOKEN GITHUB_TOKEN auth_token
printf 'Installation starten …\n'
sh "$work/scripts/$installer" "$@"
echo "Linad $version: Installation abgeschlossen."
