#!/usr/bin/env bash
set -Eeuo pipefail
# Startet nur in GitHub Actions oder lokal zum Test; keine Veröffentlichung direkt von hier.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
[[ -n "${LIGA_RCLONE_CONFIG_B64:-}" ]] || { echo 'FEHLER: GitHub Secret LIGA_RCLONE_CONFIG_B64 fehlt.' >&2; exit 2; }
REMOTE_FOLDER="${LIGA_ONEDRIVE_ORDNER:-Autodarts Liga}"
[[ "$REMOTE_FOLDER" != *$'\n'* && "$REMOTE_FOLDER" != *$'\r'* ]] || { echo 'FEHLER: Ungültiger OneDrive-Ordnername.' >&2; exit 2; }
WORK="$(mktemp -d)"
HELPER_PID=""
cleanup() {
    if [[ -n "$HELPER_PID" ]]; then kill "$HELPER_PID" >/dev/null 2>&1 || true; wait "$HELPER_PID" >/dev/null 2>&1 || true; fi
    rm -rf "$WORK"
}
trap cleanup EXIT
mkdir -p "$WORK/config" "$WORK/app/Liga-Daten" "$WORK/usercfg/AutodartsLigaSync"
chmod 700 "$WORK/config" "$WORK/usercfg" "$WORK/usercfg/AutodartsLigaSync"
printf '%s' "$LIGA_RCLONE_CONFIG_B64" | base64 --decode > "$WORK/config/rclone.conf" || { echo 'FEHLER: Rclone-Secret nicht korrekt Base64-kodiert.' >&2; exit 2; }
chmod 600 "$WORK/config/rclone.conf"
# Fail-closed: rclone copy lädt nur herunter und verändert OneDrive nicht.
if ! rclone --config "$WORK/config/rclone.conf" listremotes | grep -qx 'liga-onedrive:'; then
    echo 'FEHLER: Secret muss die Rclone-Verbindung [liga-onedrive] enthalten.' >&2
    exit 2
fi
printf 'Lade Liga-Daten aus OneDrive (nur lesend)...\n'
rclone --config "$WORK/config/rclone.conf" copy \
    "liga-onedrive:${REMOTE_FOLDER}/Liga-Daten" "$WORK/app/Liga-Daten" \
    --retries 3 --low-level-retries 3 --log-level ERROR
python3 "$ROOT/cloud/scripts/validate-data.py" "$WORK/app/Liga-Daten"

# Unveränderte Berechnung/HTML-Vorlage aus Sync Helper 0.9.3 nutzen.
printf 'Baue die Liga-Übersicht mit dem bestehenden Go-Auswerter...\n'
(cd "$ROOT/cloud/helper" && GO111MODULE=off go build -o "$WORK/AutodartsLigaSyncHelper" .)
python3 - "$WORK/usercfg/AutodartsLigaSync/config.json" "$WORK/app/Liga-Daten" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({'basePath': sys.argv[2]}), encoding='utf-8')
PY
export XDG_CONFIG_HOME="$WORK/usercfg"
"$WORK/AutodartsLigaSyncHelper" > "$WORK/helper.log" 2>&1 & HELPER_PID="$!"
READY=0
for i in $(seq 1 40); do
    if curl -fsS --connect-timeout 1 --max-time 2 http://127.0.0.1:17653/api/status >/dev/null 2>&1; then READY=1; break; fi
    if ! kill -0 "$HELPER_PID" >/dev/null 2>&1; then break; fi
    sleep 1
done
if [[ "$READY" != 1 ]]; then
    echo 'FEHLER: Der Liga-Auswerter konnte nicht gestartet werden.' >&2
    cat "$WORK/helper.log" >&2
    exit 3
fi
if ! curl -fsS -X POST --max-time 90 http://127.0.0.1:17653/api/overview -o "$WORK/overview-status.json"; then
    echo 'FEHLER: Auswertung fehlgeschlagen.' >&2
    cat "$WORK/helper.log" >&2
    exit 3
fi
python3 - "$WORK/overview-status.json" <<'PY'
import json, sys
status=json.load(open(sys.argv[1], encoding='utf-8'))
if not status.get('htmlExists'):
    sys.exit('FEHLER: Der Helper konnte keine Liga-Übersicht erzeugen.')
PY
mkdir -p "$ROOT/site"
cp "$WORK/app/Liga-Übersicht.html" "$ROOT/site/index.html"
find "$WORK/app" -maxdepth 1 -type f -name 'season-*.html' -exec cp {} "$ROOT/site/" \;
test -s "$ROOT/site/index.html"
if ! grep -q 'AUTODARTS LIGA' "$ROOT/site/index.html"; then
    echo 'FEHLER: Ungültige HTML-Ausgabe.' >&2
    exit 4
fi
printf 'OK: index.html und %s Saisonseiten für GitHub Pages erstellt.\n' "$(find "$ROOT/site" -maxdepth 1 -name 'season-*.html' | wc -l)"
