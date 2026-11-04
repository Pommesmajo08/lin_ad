#!/bin/sh
# Per-user desktop/CLI installation; no PAM or system account changes.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
prefix=${HOME:?}/.local
remove=no
while [ "$#" -gt 0 ]; do
 case "$1" in
  --prefix) [ "$#" -ge 2 ] && [ -n "$2" ] || { echo 'Wert für --prefix fehlt.' >&2; exit 1; }; prefix=$2; shift 2;;
  --uninstall) remove=yes; shift;;
  --help|-h) echo 'sh scripts/install-client-linux.sh [--prefix /ABSOLUTER/PFAD] [--uninstall]'; echo 'Installiert GUI, CLI und Startmenüeintrag für den aktuellen Benutzer.'; exit 0;;
  *) echo "Unbekannte Option: $1" >&2; exit 1;;
 esac
done
case "$prefix" in /*) ;; *) echo '--prefix muss absolut sein.' >&2; exit 1;; esac
if [ "$remove" = yes ]; then
 rm -f "$prefix/bin/linad-client" "$prefix/bin/linad-client-gui" "$prefix/share/applications/linad.desktop" "$prefix/share/icons/hicolor/scalable/apps/linad.svg"
 echo 'Linad-GUI und CLI entfernt. Server- und Gerätekonfiguration bleiben erhalten.'; exit 0
fi
case "$(uname -m)" in x86_64) ;; *) echo 'GUI-Paket unterstützt derzeit Linux AMD64.' >&2; exit 1;; esac
for file in linad-client-linux-amd64 linad-client-gui-linux-amd64; do
 [ -f "$root/dist/$file" ] || { echo "Paket unvollständig: dist/$file fehlt." >&2; exit 1; }
done
install -d "$prefix/bin" "$prefix/share/applications" "$prefix/share/icons/hicolor/scalable/apps"
install -m 0755 "$root/dist/linad-client-linux-amd64" "$prefix/bin/linad-client"
install -m 0755 "$root/dist/linad-client-gui-linux-amd64" "$prefix/bin/linad-client-gui"
install -m 0644 "$root/packaging/client/linad.svg" "$prefix/share/icons/hicolor/scalable/apps/linad.svg"
# Desktop Exec has its own quoting rules, distinct from shell quoting.
exec_path=$(printf '%s' "$prefix/bin/linad-client-gui" | sed 's/[\\"`$]/\\&/g')
cat > "$prefix/share/applications/linad.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Linad
Comment=Verzeichnisverwaltung und Gerätezugriff
Exec="$exec_path"
Icon=linad
Terminal=false
Categories=System;Settings;
StartupNotify=false
DESKTOP
chmod 0644 "$prefix/share/applications/linad.desktop"
if command -v update-desktop-database >/dev/null; then update-desktop-database "$prefix/share/applications"; fi
printf 'Linad installiert. Im Anwendungsmenü „Linad“ öffnen.\nDirektstart: "%s/bin/linad-client-gui"\nCLI: "%s/bin/linad-client"\n' "$prefix" "$prefix"
