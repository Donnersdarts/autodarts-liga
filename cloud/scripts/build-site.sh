#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
[[ -n "${LIGA_RCLONE_CONFIG_B64:-}" ]] || { echo 'FEHLER: GitHub Secret LIGA_RCLONE_CONFIG_B64 fehlt.' >&2; exit 2; }
REMOTE_FOLDER="${LIGA_ONEDRIVE_ORDNER:-Autodarts Liga}"
[[ "$REMOTE_FOLDER" != *$'\n'* && "$REMOTE_FOLDER" != *$'\r'* ]] || { echo 'FEHLER: Ungültiger OneDrive-Ordnername.' >&2; exit 2; }

WORK="$(mktemp -d)"
HELPER_PID=""
cleanup() {
    if [[ -n "$HELPER_PID" ]]; then
        kill "$HELPER_PID" >/dev/null 2>&1 || true
        wait "$HELPER_PID" >/dev/null 2>&1 || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT

mkdir -p "$WORK/config" "$WORK/app/Liga-Daten" "$WORK/usercfg/AutodartsLigaSync"
chmod 700 "$WORK/config" "$WORK/usercfg" "$WORK/usercfg/AutodartsLigaSync"
printf '%s' "$LIGA_RCLONE_CONFIG_B64" | base64 --decode > "$WORK/config/rclone.conf" || {
    echo 'FEHLER: Rclone-Secret nicht korrekt Base64-kodiert.' >&2
    exit 2
}
chmod 600 "$WORK/config/rclone.conf"

if ! rclone --config "$WORK/config/rclone.conf" listremotes | grep -qx 'liga-onedrive:'; then
    echo 'FEHLER: Secret muss die Rclone-Verbindung [liga-onedrive] enthalten.' >&2
    exit 2
fi

echo '1/5 OneDrive wird vollständig gelesen ...'
rclone --config "$WORK/config/rclone.conf" copy \
    "liga-onedrive:${REMOTE_FOLDER}/Liga-Daten" "$WORK/app/Liga-Daten" \
    --retries 3 --low-level-retries 3 --log-level ERROR

echo '2/5 OneDrive-Daten werden geprüft ...'
python3 "$ROOT/cloud/scripts/validate-data.py" "$WORK/app/Liga-Daten"

# Fingerprint wird NACH abgeschlossenem rclone-copy und VOR dem Webseitenbau erzeugt.
DATA_FINGERPRINT="$(python3 - "$WORK/app/Liga-Daten" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
h = hashlib.sha256()
count = 0
for p in sorted(x for x in root.rglob('*') if x.is_file()):
    rel = p.relative_to(root).as_posix().encode('utf-8')
    h.update(rel); h.update(b'\0')
    with p.open('rb') as f:
        while True:
            b=f.read(1024*1024)
            if not b: break
            h.update(b)
    h.update(b'\0')
    count += 1
print(f"{h.hexdigest()} {count}")
PY
)"
DATA_SHA="${DATA_FINGERPRINT%% *}"
DATA_FILES="${DATA_FINGERPRINT##* }"
MATCH_FILES="$(find "$WORK/app/Liga-Daten/Matches" -maxdepth 1 -type f -name '*.json' 2>/dev/null | wc -l | tr -d ' ')"

echo '3/5 Cloud-Helper 0.9.9 wird gebaut ...'
(cd "$ROOT/cloud/helper" && GO111MODULE=off go test ./... && GO111MODULE=off go build -o "$WORK/AutodartsLigaSyncHelper" .)

python3 - "$WORK/usercfg/AutodartsLigaSync/config.json" "$WORK/app/Liga-Daten" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({'basePath': sys.argv[2]}), encoding='utf-8')
PY
export XDG_CONFIG_HOME="$WORK/usercfg"
"$WORK/AutodartsLigaSyncHelper" > "$WORK/helper.log" 2>&1 &
HELPER_PID="$!"

READY=0
for _ in $(seq 1 40); do
    if curl -fsS --connect-timeout 1 --max-time 2 http://127.0.0.1:17653/api/status >/dev/null 2>&1; then
        READY=1
        break
    fi
    if ! kill -0 "$HELPER_PID" >/dev/null 2>&1; then break; fi
    sleep 1
done
if [[ "$READY" != 1 ]]; then
    echo 'FEHLER: Der Liga-Auswerter konnte nicht gestartet werden.' >&2
    cat "$WORK/helper.log" >&2
    exit 3
fi

echo '4/5 Liga-Webseite inklusive Finalspieltag wird neu erzeugt ...'
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

# Nie Dateien eines alten Runs weiterverwenden.
rm -rf "$ROOT/site"
mkdir -p "$ROOT/site"
cp "$WORK/app/Liga-Übersicht.html" "$ROOT/site/index.html"
find "$WORK/app" -maxdepth 1 -type f -name 'season-*.html' -exec cp {} "$ROOT/site/" \;

# Spielplan aus OneDrive/Liga-Daten/Spielplan.csv wieder einfügen.
# Nur lesend: die CSV und Matchdaten im OneDrive-Arbeitskopie werden nicht verändert.
echo '4b/5 Spielplan wird aus OneDrive in die Homepage eingefügt ...'
python3 "$ROOT/cloud/scripts/insert-spielplan.py" "$ROOT/site" "$WORK/app/Liga-Daten"

# Vorhandenen Auto-Refresh aus dem bisherigen Cloud-Stand weiterverwenden, falls installiert.
if [[ -f "$ROOT/cloud/scripts/auto-refresh.js" && -f "$ROOT/cloud/scripts/enable-auto-refresh.py" ]]; then
    cp "$ROOT/cloud/scripts/auto-refresh.js" "$ROOT/site/auto-refresh.js"
    python3 "$ROOT/cloud/scripts/enable-auto-refresh.py" "$ROOT/site" "$WORK/app/Liga-Daten"
fi

test -s "$ROOT/site/index.html"
grep -q 'AUTODARTS LIGA' "$ROOT/site/index.html"
grep -q 'Finalspieltag' "$ROOT/site/index.html" || {
    echo 'FEHLER: Die erzeugte Seite enthält keine Finalspieltag-Logik. Cloud-Helper ist nicht aktuell.' >&2
    exit 4
}
grep -q 'Saison-Awards' "$ROOT/site/index.html" || {
    echo 'FEHLER: Die erzeugte Seite enthält keine Saison-Awards.' >&2
    exit 4
}
grep -q 'LIVE · aktueller Stand' "$ROOT/site/index.html" || grep -q 'ENDSTAND · reguläre Saison' "$ROOT/site/index.html" || {
    echo 'FEHLER: Award-Stand (LIVE/ENDSTAND) fehlt in der erzeugten Seite.' >&2
    exit 4
}
grep -q 'id="spielplan"' "$ROOT/site/index.html" || {
    echo 'FEHLER: Der Spielplan wurde nicht in die Homepage eingebaut.' >&2
    exit 4
}

python3 - "$ROOT/site/build-info.json" "$DATA_SHA" "$DATA_FILES" "$MATCH_FILES" <<'PY'
import datetime, json, os, pathlib, sys
out = pathlib.Path(sys.argv[1])
data = {
    'format': 'autodarts-liga-cloud-build',
    'version': '0.9.9',
    'generatedAtUtc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'githubSha': os.environ.get('GITHUB_SHA',''),
    'oneDriveDataSha256': sys.argv[2],
    'oneDriveFileCount': int(sys.argv[3]),
    'regularMatchFileCount': int(sys.argv[4]),
    'finalspieltag': True,
    'spielplan': True,
}
out.write_text(json.dumps(data, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
PY

echo '5/5 Build ist vollständig. Erst jetzt darf GitHub Pages veröffentlichen.'
echo "OneDrive-Fingerprint: $DATA_SHA"
echo "OneDrive-Dateien: $DATA_FILES | reguläre Matchdateien: $MATCH_FILES"
echo "Saisonseiten: $(find "$ROOT/site" -maxdepth 1 -name 'season-*.html' | wc -l | tr -d ' ')"
