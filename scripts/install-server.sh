#!/bin/sh
# Linux Docker setup. Configuration and secrets live outside the checkout.
set -eu
umask 077
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
config_dir=/etc/linad-server
image_file=
start=yes
check=no
fail() { printf 'Fehler: %s\n' "$*" >&2; exit 1; }
docker_local() { env -u DOCKER_HOST -u DOCKER_CONTEXT -u DOCKER_TLS -u DOCKER_TLS_VERIFY -u DOCKER_CERT_PATH docker --host=unix:///var/run/docker.sock "$@"; }
while [ "$#" -gt 0 ]; do
 case "$1" in
  --config-dir|--image-file)
   [ "$#" -ge 2 ] && [ -n "$2" ] || fail "Wert für $1 fehlt."
   case "$2" in --*) fail "Wert für $1 fehlt.";; esac
   case "$1" in --config-dir) config_dir=$2;; --image-file) image_file=$2;; esac; shift 2;;
  --no-start) start=no; shift;;
  --check) check=yes; shift;;
  --help|-h) cat <<'HELP'
Linad Docker-Server (Linux)
  sudo sh scripts/install-server.sh
  --check               Voraussetzungen prüfen, keine Änderungen
  --no-start            Nur externe Konfiguration und Bootstrap-Passwort anlegen
  --config-dir /PFAD    Konfigurationsordner (Standard /etc/linad-server)
  --image-file DATEI    Server-Image-TAR laden (sonst dist/linad-server-image.tar)
Server: http://127.0.0.1:8080; Netzwerkzugriff über eigenen HTTPS-Reverse-Proxy.
Docker und Compose v2 müssen installiert sein. Bestehende Daten bleiben erhalten.
HELP
   exit 0;;
  *) fail "Unbekannte Option: $1";;
 esac
done
[ "$(id -u)" = 0 ] || fail 'Bitte mit sudo oder als Root starten.'
case "$config_dir" in /*) ;; *) fail '--config-dir muss absolut sein.';; esac
config_dir=$(realpath -m -- "$config_dir")
case "$config_dir/" in "$root/"*) fail 'Konfiguration muss außerhalb des Repositorys liegen.';; esac
[ ! -L "$config_dir" ] || fail 'Konfigurationsordner darf kein symbolischer Link sein.'
command -v flock >/dev/null || fail 'flock fehlt (Paket util-linux).'
command -v openssl >/dev/null || fail 'openssl fehlt (für das zufällige Bootstrap-Passwort).'
if [ -d "$config_dir" ] && [ ! -f "$config_dir/compose.yaml" ] && [ -n "$(ls -A "$config_dir")" ]; then
 fail 'Konfigurationsordner enthält fremde oder unvollständige Dateien. Einen eigenen leeren Ordner verwenden oder die bestehende Einrichtung prüfen.'
fi
if [ -f "$config_dir/compose.yaml" ]; then
 head -n 1 "$config_dir/compose.yaml" | grep -qxF '# Managed by linad-server installer' || fail 'Vorhandene Compose-Datei gehört nicht zu diesem Installer.'
fi
if [ "$start" = yes ]; then
 command -v curl >/dev/null || fail 'curl fehlt (für den Bereitschaftstest). Hinweis: sudo apt-get install curl'
 command -v docker >/dev/null || fail 'Docker fehlt. Zuerst Docker Engine und Compose v2 installieren.'
 docker_local info >/dev/null 2>&1 || fail 'Docker-Daemon nicht erreichbar.'
 docker_local compose version >/dev/null 2>&1 || fail 'Docker Compose v2 fehlt.'
 if [ -z "$image_file" ] && [ -f "$root/dist/linad-server-image.tar" ]; then image_file=$root/dist/linad-server-image.tar; fi
 if [ -n "$image_file" ]; then
  [ -r "$image_file" ] || fail 'Image-TAR nicht lesbar.'
 elif ! docker_local image inspect linad-server:local >/dev/null 2>&1; then
  fail 'Server-Image fehlt. Image-TAR mit --image-file angeben oder zuerst bauen.'
 fi
fi
[ "$check" = no ] || { echo 'Vorabprüfung erfolgreich; keine Änderungen vorgenommen.'; exit 0; }
exec 9>/run/linad-server-install.lock
flock -n 9 || fail 'Eine weitere Linad-Serverinstallation läuft bereits.'
install -d -m 0750 "$config_dir"
[ ! -L "$config_dir/admin-password" ] || fail 'Bootstrap-Secret darf kein symbolischer Link sein.'
if [ ! -e "$config_dir/admin-password" ]; then
 openssl rand -hex 32 | tr -d '\n' > "$config_dir/admin-password"
fi
[ -f "$config_dir/admin-password" ] && [ ! -L "$config_dir/admin-password" ] || fail 'Bootstrap-Secret muss eine reguläre Datei sein.'
[ "$(wc -c < "$config_dir/admin-password")" -ge 16 ] || fail 'Vorhandenes Bootstrap-Secret ist zu kurz.'
chown 65532:65532 "$config_dir/admin-password"
chmod 0400 "$config_dir/admin-password"
if [ ! -f "$config_dir/compose.yaml" ]; then
 cat > "$config_dir/compose.yaml" <<'COMPOSE'
# Managed by linad-server installer
name: lin_ad
services:
  directory:
    image: linad-server:local
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      LINAD_ADMIN_PASSWORD_FILE: /run/secrets/admin_password
    secrets:
      - admin_password
    volumes:
      - directory-data:/data
    restart: unless-stopped
secrets:
  admin_password:
    file: ./admin-password
volumes:
  directory-data:
COMPOSE
 chmod 0644 "$config_dir/compose.yaml"
fi
if [ "$start" = yes ]; then
 if [ -n "$image_file" ]; then echo '[1/2] Server-Image laden …'; docker_local load -i "$image_file"; fi
 echo '[2/2] Server starten …'
 docker_local compose -f "$config_dir/compose.yaml" up -d --wait --wait-timeout 60
 binding=$(docker_local compose -f "$config_dir/compose.yaml" port directory 8080)
 port=${binding##*:}
 case "$port" in ''|*[!0-9]*) fail 'Veröffentlichten HTTP-Port nicht erkannt.';; esac
 server_url=http://127.0.0.1:$port
 ready=no
 for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if curl --noproxy localhost,127.0.0.1 --silent --fail --max-time 2 "$server_url/healthz" | grep -q '"status":"ok"'; then ready=yes; break; fi
  sleep 1
 done
 [ "$ready" = yes ] || fail "Server nicht bereit. Protokoll: sudo docker compose -f $config_dir/compose.yaml logs"
 docker_local compose -f "$config_dir/compose.yaml" ps
fi
printf '\nLinad-Server eingerichtet. Adresse: %s\n' "${server_url:-http://127.0.0.1:8080}"
printf 'Konfiguration: %s/compose.yaml\n' "$config_dir"
printf 'Erster Login: admin. Bootstrap-Passwort lokal anzeigen: sudo cat "%s/admin-password"\n' "$config_dir"
echo 'Bei vorhandener Datenbank gilt das bisherige Admin-Passwort. Das Bootstrap-Secret setzt kein Passwort zurück.'
echo 'Für entfernte Geräte zuerst HTTPS über einen Reverse-Proxy einrichten.'
printf 'Status: sudo docker compose -f "%s/compose.yaml" ps\n' "$config_dir"
