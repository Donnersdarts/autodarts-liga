#!/usr/bin/env python3
"""Fail-closed validation: niemals mit leerem/falschem OneDrive den Live-Stand ersetzen."""
import json
import pathlib
import re
import sys

base = pathlib.Path(sys.argv[1])
season_file = base / 'Seasons.json'
if not season_file.is_file():
    sys.exit('FEHLER: Seasons.json fehlt im synchronisierten Liga-Daten-Ordner. Veröffentlichung abgebrochen.')
try:
    seasons = json.loads(season_file.read_text(encoding='utf-8'))
except (ValueError, OSError) as exc:
    sys.exit(f'FEHLER: Seasons.json ist nicht lesbar: {exc}')
if seasons.get('format') != 'autodarts-match-analytics-seasons' or not seasons.get('seasons'):
    sys.exit('FEHLER: Ungültige Saisondaten; Veröffentlichung abgebrochen.')
name = str(seasons.get('leagueName', '')).strip()
current = str(seasons.get('currentSeasonId', '')).strip()
season_ids = {str(s.get('id', '')) for s in seasons['seasons']}
if not name or not current or current not in season_ids:
    sys.exit('FEHLER: Ligabezeichnung oder aktive Saison fehlt.')
match_dir = base / 'Matches'
if not match_dir.is_dir():
    sys.exit('FEHLER: Zentraler Matches-Ordner fehlt.')
valid = 0
invalid = 0
active = 0
seen = set()
uuid_re = re.compile(r'^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$')
for path in match_dir.glob('*.json'):
    try:
        obj = json.loads(path.read_text(encoding='utf-8'))
        if obj.get('format') != 'autodarts-match-analytics-league-match':
            invalid += 1
            continue
        if str(obj.get('league', {}).get('name', '')).strip().casefold() != name.casefold():
            invalid += 1
            continue
        match = obj['match']
        mid = str(match['matchId'])
        if not uuid_re.fullmatch(mid) or match.get('usageMode') != 'league' or not 1 <= int(match.get('matchday', 0)) <= 10:
            invalid += 1
            continue
        if mid in seen:
            continue
        seen.add(mid)
        valid += 1
        if str(match.get('seasonId', '')) == current:
            active += 1
    except (ValueError, OSError, TypeError, AttributeError, KeyError):
        invalid += 1
if valid < 1:
    sys.exit('FEHLER: Keine gültigen Ligamatches gefunden. Bestehende Webseite bleibt erhalten.')
print(f'Validierung OK: Liga={name!r}, Saison={current!r}, Matches={valid}, aktive Saison Matches={active}, übersprungene Dateien={invalid}.')
# Archivierte Saison darf aktiv leer sein; dabei bleiben die Historie und Saisonwahl gültig.
