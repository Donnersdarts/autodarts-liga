#!/usr/bin/env python3
# Autodarts Liga: sichere Scoring-Wiederherstellung aus index.html (Stand 05.10.2026).
# GitHub-Actions-Variante: nutzt denselben OneDrive-Remote wie der Cloud-Publisher.
# Standard: NUR PRUEFEN. Schreibzugriffe nur mit ausdruecklichem --apply.
# Keine GitHub-Geheimnisse und keine OneDrive-Tokens in dieser Datei.
import argparse
import datetime as dt
import hashlib
import json
import subprocess
import sys
import tempfile
from pathlib import Path

PATCHES = {
  "01a08cc4-3bd5-7ed8-a789-f63663a4bd52": {
    "score": [
      0,
      4
    ],
    "players": [
      {
        "name": "MARCO",
        "aliases": [
          "MARCO",
          "motto87"
        ],
        "metrics": {
          "scores57Plus": 9,
          "scores95Plus": 1,
          "scores114Plus": 1,
          "scores133Plus": 1,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      },
      {
        "name": "HAUKE",
        "aliases": [
          "HAUKE",
          "HAUKE"
        ],
        "metrics": {
          "scores57Plus": 13,
          "scores95Plus": 0,
          "scores114Plus": 1,
          "scores133Plus": 0,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      }
    ]
  },
  "01a08ca1-5f7f-7100-a019-743ebab488c5": {
    "score": [
      1,
      4
    ],
    "players": [
      {
        "name": "JULI",
        "aliases": [
          "JULI",
          "JULI"
        ],
        "metrics": {
          "scores57Plus": 11,
          "scores95Plus": 1,
          "scores114Plus": 1,
          "scores133Plus": 0,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      },
      {
        "name": "SVEN",
        "aliases": [
          "SVEN",
          "SVEN"
        ],
        "metrics": {
          "scores57Plus": 8,
          "scores95Plus": 2,
          "scores114Plus": 2,
          "scores133Plus": 0,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      }
    ]
  },
  "01a0b0df-27fc-7bfe-b41e-b085b4b60280": {
    "score": [
      4,
      1
    ],
    "players": [
      {
        "name": "SVEN",
        "aliases": [
          "SVEN",
          "the unfroggettable"
        ],
        "metrics": {
          "scores57Plus": 16,
          "scores95Plus": 2,
          "scores114Plus": 0,
          "scores133Plus": 0,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      },
      {
        "name": "HAUKE",
        "aliases": [
          "HAUKE",
          "HAUKE"
        ],
        "metrics": {
          "scores57Plus": 10,
          "scores95Plus": 0,
          "scores114Plus": 1,
          "scores133Plus": 1,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      }
    ]
  },
  "01a0b0b8-b98b-7e8b-8fbb-8ec2f4c9c9ec": {
    "score": [
      3,
      4
    ],
    "players": [
      {
        "name": "FLO",
        "aliases": [
          "FLO",
          "FLO"
        ],
        "metrics": {
          "scores57Plus": 14,
          "scores95Plus": 4,
          "scores114Plus": 0,
          "scores133Plus": 0,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 1
        }
      },
      {
        "name": "JULI",
        "aliases": [
          "JULI",
          "JULI"
        ],
        "metrics": {
          "scores57Plus": 17,
          "scores95Plus": 1,
          "scores114Plus": 1,
          "scores133Plus": 1,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      }
    ]
  },
  "01a0b094-d47e-762e-8c8e-4e5fd92d4e14": {
    "score": [
      4,
      3
    ],
    "players": [
      {
        "name": "SVEN",
        "aliases": [
          "SVEN",
          "the unfroggettable"
        ],
        "metrics": {
          "scores57Plus": 15,
          "scores95Plus": 4,
          "scores114Plus": 1,
          "scores133Plus": 2,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      },
      {
        "name": "MARCO",
        "aliases": [
          "MARCO",
          "MARCO"
        ],
        "metrics": {
          "scores57Plus": 19,
          "scores95Plus": 5,
          "scores114Plus": 2,
          "scores133Plus": 1,
          "scores171": 0,
          "scores174": 0,
          "scores177": 0,
          "oneEighties": 0
        }
      }
    ]
  }
}
REMOTE = 'liga-onedrive:Autodarts Liga/Liga-Daten'


def call(*cmd, inp=None):
    try:
        p = subprocess.run(cmd, input=inp, stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, check=False)
    except FileNotFoundError:
        raise RuntimeError(f'Programm nicht gefunden: {cmd[0]}')
    if p.returncode:
        msg = p.stderr.decode('utf-8', errors='replace')
        raise RuntimeError(f'Fehler bei {cmd[0]} ({p.returncode}): {msg[:600]}')
    return p.stdout


def read_remote(path):
    return call('rclone', 'cat', path)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def match_name(expected, aliases, record):
    known = {expected.casefold()}
    known.update(x.casefold() for x in aliases if x)
    variants = [record.get(k, '') for k in ('name', 'realName', 'autodartsName')]
    return any(isinstance(x, str) and x.casefold() in known for x in variants)


def patch_one(mid, original, spec):
    root = json.loads(original)
    if not isinstance(root, dict) or not isinstance(root.get('match'), dict):
        raise ValueError(f'{mid}: unerwartete JSON-Struktur (match fehlt)')
    m = root['match']
    if m.get('matchId', '').lower() != mid.lower():
        raise ValueError(f'{mid}: Match-ID passt nicht zur Datei')
    if m.get('usageMode') != 'league':
        raise ValueError(f'{mid}: nicht als Liga-Match markiert')
    if m.get('score') != spec['score']:
        raise ValueError(f'{mid}: Spielergebnis weicht vom alten Original ab')
    players = m.get('players')
    if not isinstance(players, list) or len(players) != 2:
        raise ValueError(f'{mid}: keine zwei Spieler im erwarteten Format')
    changed=[]
    for n, (p, oldp) in enumerate(zip(players, spec['players']), 1):
        if not isinstance(p, dict) or not match_name(oldp['name'], oldp['aliases'], p):
            raise ValueError(f'{mid}: Spieler {n} stimmt nicht ueberein')
        metrics=p.get('metrics')
        if not isinstance(metrics,dict):
            raise ValueError(f'{mid}: metrics bei Spieler {n} fehlen')
        for k,v in oldp['metrics'].items():
            previous=metrics.get(k)
            if previous is None:
                metrics[k]=v
                changed.append((n,k,v))
            elif isinstance(previous,bool) or not isinstance(previous,(int,float)) or previous!=v:
                raise ValueError(f'{mid}: KONFLIKT bei Spieler {n}, {k}: OneDrive={previous!r}, alt={v!r}; nichts ueberschreiben!')
    if not changed:
        return original, []
    # Nur fehlende Statistikfelder wurden geaendert. Die restliche JSON-Struktur bleibt erhalten.
    after=(json.dumps(root,indent=2,ensure_ascii=False)+'\n').encode('utf-8')
    return after, changed


def main():
    cli=argparse.ArgumentParser(description='Scoring von 5 Liga-Matches pruefen und bei Freigabe sicher ergaenzen')
    cli.add_argument('--apply',action='store_true',help='Schreibzugriff nach Backup freigeben')
    cli.add_argument('--confirm', default='', help='Sicherheitsbestaetigung bei --apply')
    args=cli.parse_args()
    if args.apply and args.confirm != 'WIEDERHERSTELLEN':
        raise ValueError('Schreibmodus nicht bestaetigt; ohne korrekte Bestaetigung kein Schreiben')
    data={}; changed_total=0
    print('OneDrive-Bereich:',REMOTE)
    print('Modus:', 'REPARIEREN (--apply)' if args.apply else 'NUR PRUEFEN (keine Aenderung)')
    for mid,spec in PATCHES.items():
        path=f'{REMOTE}/Matches/{mid}.json'
        original=read_remote(path)
        amended, changes=patch_one(mid, original, spec)
        changed_total+=len(changes)
        data[mid]={'path':path,'original':original,'patched':amended,'changes':changes}
        label=' / '.join(p['name'] for p in spec['players'])
        print(f'{label} [{mid[:8]}]: {len(changes)} fehlende Werte')
    print(f'Gesamt: {changed_total} fehlende Werte in {len(PATCHES)} kontrollierten Spielen.')
    if not args.apply:
        print('ERFOLG: Pruefung beendet. Es wurde NICHTS veraendert.')
        print('Erst nach Freigabe: Workflow-Modus reparieren mit Sicherheitsbestaetigung')
        return
    if changed_total==0:
        print('Alle Statistikwerte sind bereits vorhanden. Keine Aenderung erforderlich.')
        return
    stamp=dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    backup=f'{REMOTE}/Backup-Scoring-{stamp}'
    # VOR DER REPARATUR ALLE originalen Matchdateien sichern und die Sicherung verifizieren.
    print('Erstelle sichere OneDrive-Kopie der betroffenen Dateien ...')
    call('rclone','mkdir',backup)
    for mid,entry in data.items():
        dst=f'{backup}/{mid}.json'
        call('rclone','copyto',entry['path'],dst)
        if digest(read_remote(dst))!=digest(entry['original']):
            raise RuntimeError(f'{mid}: Backup stimmt nicht mit dem Original ueberein! ABGEBROCHEN')
    print('Backup vollstaendig und geprueft:',backup)
    # Alle Quelldateien nochmals pruefen, falls zwischendurch ein Spieler neue Daten hochgeladen hat.
    for mid,entry in data.items():
        if digest(read_remote(entry['path']))!=digest(entry['original']):
            raise RuntimeError(f'{mid}: OneDrive wurde zwischenzeitlich geaendert. ABBRUCH ohne Reparatur.')
    finished=[]
    with tempfile.TemporaryDirectory(prefix='autodarts-repair-') as folder:
        for mid,entry in data.items():
            if not entry['changes']:
                continue
            local=Path(folder)/f'{mid}.json'
            local.write_bytes(entry['patched'])
            call('rclone','copyto',str(local),entry['path'])
            if digest(read_remote(entry['path']))!=digest(entry['patched']):
                raise RuntimeError(f'{mid}: Schreibpruefung fehlgeschlagen. Backup: {backup}')
            finished.append(mid)
            print('Repariert und verifiziert:',mid[:8])
    print(f'ERFOLG: {len(finished)} Spiele korrigiert; alle alten Dateien gesichert unter:')
    print(backup)
    print('Die Cloud-Webseite wird beim naechsten erfolgreichen Publish neu erstellt.')


if __name__=='__main__':
    try:
        main()
    except (ValueError, RuntimeError, OSError, json.JSONDecodeError) as e:
        print('ABBRUCH:',e,file=sys.stderr)
        sys.exit(1)
