#!/bin/sh
# Debian/Ubuntu root provisioning; token is read by the agent without echo.
set -eu
umask 077
linad_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
linad_start=yes
linad_pam=yes
linad_server=
linad_device=
linad_ca=
linad_http=no
linad_check=no
linad_stage=Vorabprüfung
fail() { printf '\nFehler: %s\n' "$*" >&2; exit 1; }
usage() {
 cat <<'HELP'
Linad Linux-Geräteinstallation (Debian/Ubuntu)
  sudo sh scripts/install-linux.sh             Geführte Einrichtung / Update
  sudo sh scripts/install-linux.sh --check     Nur Vorabprüfung, keine Änderungen
Optionen:
  --server URL       Verzeichnisserver (HTTPS)
  --device-id ID     Geräte-ID aus Geräte → Linux provisionieren
  --ca-file DATEI    Optionales CA-Zertifikat
  --allow-http       Unverschlüsseltes HTTP ausdrücklich erlauben
  --no-start         Image-Provisionierung ohne laufendes systemd
  --no-pam           Gemeinsames Anmeldeprofil noch nicht aktivieren
  --help             Diese Hilfe
Updates verwenden den vorhandenen Geräteschlüssel, ohne neuen Token.
HELP
}
while [ "$#" -gt 0 ]; do
 case "$1" in
  --server|--device-id|--ca-file)
   [ "$#" -ge 2 ] && [ -n "$2" ] || fail "Wert für $1 fehlt. Siehe --help."
   case "$2" in --*) fail "Wert für $1 fehlt.";; esac
   case "$1" in --server) linad_server=$2;; --device-id) linad_device=$2;; --ca-file) linad_ca=$2;; esac; shift 2;;
  --allow-http) linad_http=yes; shift;;
  --no-start) linad_start=no; shift;;
  --no-pam) linad_pam=no; shift;;
  --check) linad_check=yes; shift;;
  --help|-h) usage; exit 0;;
  *) fail "Unbekannte Option: $1. Siehe --help.";;
 esac
done
[ "$(id -u)" = 0 ] || fail 'Bitte mit sudo oder als Root starten.'
trap 'linad_result=$?; if [ "$linad_result" -ne 0 ]; then printf "\nInstallation abgebrochen bei: %s.\nVorhandene Geräteschlüssel werden nicht gelöscht. Nach Behebung denselben Installer erneut starten.\n" "$linad_stage" >&2; fi' 0
[ -f /etc/debian_version ] || fail 'Dieser Geräte-Installer unterstützt Debian/Ubuntu.'
linad_missing=
for linad_command in useradd usermod groupadd pam-auth-update sudo visudo flock; do
 if ! command -v "$linad_command" >/dev/null; then linad_missing="$linad_missing $linad_command"; fi
done
[ -z "$linad_missing" ] || fail "Voraussetzungen fehlen:$linad_missing. Benötigte Pakete: passwd libpam-runtime libpam-modules sudo util-linux. Bei unterbrochenem dpkg zuerst 'sudo dpkg --configure -a' ausführen."
if [ "$linad_start" = yes ]; then command -v systemctl >/dev/null || fail 'systemctl fehlt.'; [ -d /run/systemd/system ] || fail 'systemd läuft nicht. Für Image-Provisionierung --no-start verwenden.'; fi
case "$(uname -m)" in x86_64) linad_arch=amd64;; aarch64|arm64) linad_arch=arm64;; *) fail 'Architektur nicht unterstützt (AMD64/ARM64 erforderlich).';; esac
linad_binary=$linad_root/dist/linad-linux-$linad_arch
[ -x "$linad_binary" ] || fail "Agent fehlt oder ist nicht ausführbar: $linad_binary. Das vollständige Provisioner-Paket entpacken."
for linad_file in linad-sudo.pam-service linad.pam-service linad.pam-profile linad-linux.service; do
 [ -r "$linad_root/packaging/linux/$linad_file" ] || fail "Paketdatei fehlt: $linad_file."
done
if [ -e /etc/pam.d/linad-sudo ] && ! head -n 1 /etc/pam.d/linad-sudo | grep -qxF '# Managed by linad-linux'; then fail 'Bestehender sudo-PAM-Dienst gehört nicht zu Linad; bitte manuell prüfen.'; fi
if [ -f /usr/share/pam-configs/linad ] && ! cmp -s /usr/share/pam-configs/linad "$linad_root/packaging/linux/linad.pam-profile"; then fail 'Bestehendes PAM-Profil wurde verändert; vor Ersetzung prüfen.'; fi
printf '\nLinad · Linux-Gerät (%s)\n' "$linad_arch"
if [ -e /etc/linad-linux/client.json ]; then
 echo 'Modus: Aktualisierung; vorhandene Konfiguration wird wiederverwendet.'
 "$linad_binary" doctor
else
 echo 'Modus: Ersteinrichtung. Geräte-ID und Token findest du in der Webverwaltung unter Geräte → Linux provisionieren.'
 if [ -t 0 ]; then
  if [ -z "$linad_server" ]; then printf 'Serveradresse (https://…): '; IFS= read -r linad_server; fi
  if [ -z "$linad_device" ]; then printf 'Geräte-ID: '; IFS= read -r linad_device; fi
  if [ -z "$linad_ca" ]; then printf 'Eigene CA-Datei (optional, Enter für Systemzertifikate): '; IFS= read -r linad_ca; fi
 fi
 [ -n "$linad_server" ] && [ -n "$linad_device" ] || fail '--server und --device-id fehlen. Interaktiv ohne Pipe starten oder beide Optionen angeben.'
 set -- check-connection --server "$linad_server" --device-id "$linad_device"
 if [ -n "$linad_ca" ]; then [ -r "$linad_ca" ] || fail 'CA-Datei nicht lesbar.'; set -- "$@" --ca-file "$linad_ca"; fi
 if [ "$linad_http" = yes ]; then echo 'Hinweis: HTTP überträgt Passwörter und Geräteschlüssel unverschlüsselt.'; set -- "$@" --allow-http; fi
 "$linad_binary" "$@"
fi
if command -v dpkg >/dev/null; then
 linad_audit=$(dpkg --audit 2>&1) || fail 'Paketdatenbank konnte nicht geprüft werden.'
 [ -z "$linad_audit" ] || fail "dpkg meldet unvollständige Paketinstallationen. Zuerst 'sudo dpkg --configure -a' ausführen."
fi
[ "$linad_check" = no ] || { echo 'Vorabprüfung erfolgreich. Keine Dateien oder Konten verändert; kein Token eingelöst.'; exit 0; }
# flock is released even if the installer is interrupted.
exec 9>/run/linad-linux-install.lock
flock -n 9 || fail 'Eine weitere Linad-Installation läuft bereits.'
linad_stage='Agent und Geräteschlüssel'
echo '[1/4] Agent installieren und Gerät verbinden …'
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
linad_stage='sudo-PAM und Kontensynchronisierung'
echo '[2/4] Konten synchronisieren …'
if [ -e /etc/pam.d/linad-sudo ] && ! head -n 1 /etc/pam.d/linad-sudo | grep -qxF '# Managed by linad-linux'; then echo 'Existing sudo PAM service is not managed by linad.' >&2; exit 1; fi
install -o root -g root -m 0644 "$linad_root/packaging/linux/linad-sudo.pam-service" /etc/pam.d/linad-sudo
/usr/local/libexec/linad-linux pam-install-sudo
# Successful sync is required before modifying any active PAM configuration.
/usr/local/libexec/linad-linux sync
if [ -f /usr/share/pam-configs/linad ] && ! cmp -s /usr/share/pam-configs/linad "$linad_root/packaging/linux/linad.pam-profile"; then echo 'Existing PAM profile differs; review it before replacing.' >&2; exit 1; fi
install -o root -g root -m 0644 "$linad_root/packaging/linux/linad.pam-service" /etc/pam.d/linad
linad_stage=Anmeldeintegration
echo '[3/4] Anmeldeintegration einrichten …'
if [ "$linad_pam" = yes ]; then
 install -o root -g root -m 0644 "$linad_root/packaging/linux/linad.pam-profile" /usr/share/pam-configs/linad
 linad_backup=$(mktemp -d /var/lib/linad-linux/pam-backup-XXXXXXXX)
 cp -a /etc/pam.d/common-* "$linad_backup/"
 DEBIAN_FRONTEND=noninteractive pam-auth-update --package --enable linad
 if ! grep -qF '/usr/local/libexec/linad-linux pam-auth' /etc/pam.d/common-auth || ! grep -qF '/usr/local/libexec/linad-linux pam-account' /etc/pam.d/common-account; then
  echo "PAM activation was not applied (custom PAM configuration). Original files are in $linad_backup. No forced overwrite was performed." >&2; exit 1
 fi
fi
linad_stage=Systemdienst
echo '[4/4] Systemdienst einrichten …'
install -d -m 0755 /etc/systemd/system
install -o root -g root -m 0644 "$linad_root/packaging/linux/linad-linux.service" /etc/systemd/system/linad-linux.service
if [ "$linad_start" = yes ]; then systemctl daemon-reload; systemctl enable linad-linux.service; echo 'Ein laufender Job wird vor dem Neustart abgewartet (bis zu 30 Minuten).'; systemctl restart linad-linux.service; systemctl is-active --quiet linad-linux.service || fail 'Dienst ist nicht aktiv. Mit journalctl -u linad-linux prüfen.'; fi
echo 'Linad erfolgreich installiert. Lokale Konten bleiben verfügbar.'
/usr/local/libexec/linad-linux doctor
printf '\nDiagnose: sudo /usr/local/libexec/linad-linux doctor\nProtokoll: sudo journalctl -u linad-linux\n'
if [ "$linad_pam" = no ]; then echo 'Gemeinsame PAM-Anmeldung ist noch nicht aktiviert (--no-pam).'; fi
if [ "$linad_start" = no ]; then echo 'Dienststart ausgelassen. Nach dem Boot: sudo systemctl enable --now linad-linux'; fi
