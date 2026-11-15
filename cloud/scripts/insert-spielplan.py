#!/usr/bin/env python3
"""Fügt optional einen Spielplan aus OneDrive/Liga-Daten/Spielplan.csv in GitHub-Pages-HTML ein.

Sicherheitsprinzip: nur lesend im Liga-Datenordner. Keine Änderungen an Matches,
OneDrive, Go-Auswerter oder Chrome-Addon. Fehlende CSV => freundlicher Leerzustand.
"""
import csv
import datetime as dt
import html
import json
import re
import sys
import unicodedata
from collections import defaultdict
from pathlib import Path

CSS = r"""
<style id="liga-fixtures-css">
#spielplan .plan-sub{color:#9cb0c5;font-size:13px;margin:-3px 0 16px}
#spielplan .plan-summary{display:flex;flex-wrap:wrap;gap:9px;margin:0 0 16px}
#spielplan .plan-stat{background:#0d2338;border:1px solid #29435b;border-radius:9px;padding:8px 12px;font-size:12px;color:#9bcfff}
#spielplan .plan-stat b{color:#eef6ff;font-size:17px;margin-right:4px}
#spielplan .plan-day{border:1px solid #2b4156;border-radius:12px;margin:10px 0;background:#0d1826;overflow:hidden}
#spielplan .plan-day summary{cursor:pointer;display:flex;align-items:center;gap:12px;padding:12px 14px;list-style:none;font-weight:800;color:#ecf7ff}
#spielplan .plan-day summary::-webkit-details-marker{display:none}
#spielplan .plan-day summary::before{content:'▸';color:#79bafb;font-size:15px;transition:transform .15s}
#spielplan .plan-day[open] summary::before{transform:rotate(90deg)}
#spielplan .plan-day summary small{margin-left:auto;color:#9cb0c5;font-size:12px;font-weight:500}
#spielplan .plan-list{padding:0 14px 6px}
#spielplan .plan-fixture{display:grid;grid-template-columns:minmax(0,1fr) 65px minmax(0,1fr) 110px;gap:12px;align-items:center;padding:13px 5px;border-top:1px solid #253b50}
#spielplan .plan-p1{text-align:right}#spielplan .plan-p2{text-align:left}
#spielplan .plan-player{font-weight:750;overflow-wrap:anywhere}
#spielplan .plan-result{text-align:center;font-weight:900;font-size:17px}
#spielplan .plan-pending{font-weight:600;font-size:12px;color:#8db0c9}
#spielplan .plan-state{text-align:right;color:#b5cadb;font-size:11px}
#spielplan .plan-state b{display:block;color:#6de5ac;font-size:12px}
#spielplan .plan-detail{grid-column:1/-1;text-align:center;font-size:12px;color:#8da7bd;margin-top:-6px}
#spielplan .plan-detail a{color:#8ec4ff;text-decoration:none}
#spielplan .plan-empty{border:1px dashed #365574;border-radius:10px;padding:16px;color:#9cb0c5;font-size:13px}
@media(max-width:620px){#spielplan .plan-fixture{grid-template-columns:minmax(0,1fr) 48px minmax(0,1fr);gap:6px}#spielplan .plan-state{grid-column:1/-1;text-align:center}#spielplan .plan-player{font-size:12px}#spielplan .plan-result{font-size:14px}#spielplan .plan-day summary{padding:11px 9px}}
</style>"""
HEADERS = ('Saison','Spieltag','Datum','Uhrzeit','Spieler1','Spieler2','Ort','Hinweis')
SECTION_START = '<!-- LIGA_SPIELPLAN_START -->'
SECTION_END = '<!-- LIGA_SPIELPLAN_END -->'


def norm(value):
    return ' '.join(unicodedata.normalize('NFKC', str(value or '')).casefold().split())


def text(value):
    return html.escape(str(value or ''), quote=True)


def read_schedule(folder):
    path = folder / 'Spielplan.csv'
    if not path.is_file():
        return []
    # utf-8-sig: LibreOffice/Excel exports with BOM supported.
    with path.open(encoding='utf-8-sig', newline='') as stream:
        reader = csv.DictReader(stream, delimiter=';')
        if not reader.fieldnames:
            raise ValueError('Spielplan.csv ist leer oder enthält keine Spaltenüberschriften')
        missing = set(HEADERS) - set(reader.fieldnames)
        if missing:
            raise ValueError('Spielplan.csv: Spalten fehlen: '+', '.join(sorted(missing)))
        rows=[]
        keys=set()
        for line, r in enumerate(reader, 2):
            a, b = (r.get('Spieler1') or '').strip(), (r.get('Spieler2') or '').strip()
            # Erlaubt vorbereitete leere Zeilen in der Vorlage.
            if not a and not b: continue
            if not a or not b:
                raise ValueError(f'Spielplan.csv Zeile {line}: beide Spielernamen ausfüllen')
            if norm(a)==norm(b):
                raise ValueError(f'Spielplan.csv Zeile {line}: Spieler darf nicht gegen sich selbst spielen')
            try:
                day=int((r.get('Spieltag') or '').strip())
                if day<1 or day>99: raise ValueError()
            except ValueError:
                raise ValueError(f'Spielplan.csv Zeile {line}: Spieltag muss zwischen 1 und 99 liegen')
            season=(r.get('Saison') or '').strip()
            date=(r.get('Datum') or '').strip()
            time=(r.get('Uhrzeit') or '').strip()
            if date:
                try: dt.datetime.strptime(date, '%d.%m.%Y' if '.' in date else '%Y-%m-%d')
                except ValueError: raise ValueError(f'Spielplan.csv Zeile {line}: Datum bitte TT.MM.JJJJ oder JJJJ-MM-TT')
            if time:
                try: dt.datetime.strptime(time,'%H:%M')
                except ValueError: raise ValueError(f'Spielplan.csv Zeile {line}: Uhrzeit bitte HH:MM')
            key=(norm(season),day,*sorted((norm(a),norm(b))))
            if key in keys:
                raise ValueError(f'Spielplan.csv Zeile {line}: doppelte Paarung im gleichen Spieltag')
            keys.add(key)
            rows.append({'season':season,'day':day,'date':date,'time':time,'a':a,'b':b,
                         'place':(r.get('Ort') or '').strip(),'note':(r.get('Hinweis') or '').strip()})
        return rows


def seasons(folder):
    path=folder/'Seasons.json'
    if path.is_file():
        reg=json.loads(path.read_text(encoding='utf-8-sig'))
        return (str(reg.get('currentSeasonId') or ''),
                {str(x.get('id')):str(x.get('name') or '') for x in reg.get('seasons',[]) if x.get('id')})
    return '',{}


def matches(folder):
    deleted=set()
    for f in (folder/'Deleted').glob('*.json'):
        try:
            d=json.loads(f.read_text(encoding='utf-8-sig'))
            deleted.add(norm(d.get('matchId') or f.stem))
        except (ValueError, OSError):
            raise ValueError(f'Fehlerhaftes Löschkennzeichen: {f.name}')
    found=[]
    for f in (folder/'Matches').glob('*.json'):
        obj=json.loads(f.read_text(encoding='utf-8-sig'))
        m=obj.get('match') or {}
        if m.get('usageMode')!='league' or norm(m.get('matchId')) in deleted:
            continue
        players=m.get('players') or []
        score=m.get('score')
        if len(players)!=2 or not isinstance(score,list) or len(score)!=2 or not all(type(x) is int and x>=0 for x in score):
            continue
        def aliases(p):
            return {norm(p.get(key)) for key in ('name','realName','autodartsName') if p.get(key)}
        names=[aliases(p) for p in players]
        if not all(names): continue
        winner=m.get('winner')
        if winner not in (0,1): winner=None
        found.append({'day':m.get('matchday'), 'season':m.get('seasonId') or obj.get('league',{}).get('seasonId'),
            'players':names, 'score':score, 'id':m.get('matchId'), 'league':obj.get('league',{}).get('name',''),
            'walkover':bool(m.get('walkover')), 'winner':winner})
    return found


def fixture_result(f, matches, season, league):
    compatible=[]
    for m in matches:
        if str(m['day'])!=str(f['day']): continue
        if season and m['season'] and norm(m['season'])!=norm(season): continue
        if league and m['league'] and norm(m['league'])!=norm(league): continue
        na,nb=norm(f['a']),norm(f['b'])
        if na in m['players'][0] and nb in m['players'][1]:
            winner = f['a'] if m.get('winner') == 0 else (f['b'] if m.get('winner') == 1 else '')
            compatible.append((m['score'][0],m['score'][1],m['id'],m.get('walkover',False),winner))
        elif na in m['players'][1] and nb in m['players'][0]:
            winner = f['b'] if m.get('winner') == 0 else (f['a'] if m.get('winner') == 1 else '')
            compatible.append((m['score'][1],m['score'][0],m['id'],m.get('walkover',False),winner))
    if len(compatible)==1:
        return compatible[0]
    if len(compatible)>1:
        print(f'WARNUNG: mehrdeutige Matchzuordnung bei Spieltag {f["day"]}: {f["a"]}/{f["b"]}',file=sys.stderr)
    return None


def render(rows,matches_list,season,season_name,league):
    filtered=[r for r in rows if not r['season'] or norm(r['season']) in {norm(season),norm(season_name)}]
    # Leere Saison-Kennung gehört nur zur aktiven Saison; auf Archivseiten wird sie ausgeschlossen.
    by_day=defaultdict(list)
    for f in filtered:
        result=fixture_result(f,matches_list,season,league)
        by_day[f['day']].append((f,result))
    complete=sum(bool(result) for group in by_day.values() for f,result in group)
    total=sum(len(group) for group in by_day.values())
    s=[SECTION_START,'<section class="panel" id="spielplan"><h2>Spielplan</h2>',
       '<p class="plan-sub">Begegnungen und Termine · Ergebnisse werden automatisch aus den Ligaspielen ergänzt</p>']
    if total:
        s.append(f'<div class="plan-summary"><span class="plan-stat"><b>{total}</b>Begegnungen</span><span class="plan-stat"><b>{complete}</b>gespielt</span><span class="plan-stat"><b>{total-complete}</b>offen</span></div>')
        first_open=min((d for d,g in by_day.items() if any(r is None for _,r in g)),default=min(by_day))
        for day,group in sorted(by_day.items()):
            played=sum(r is not None for f,r in group)
            s.append(f'<details class="plan-day"{" open" if day==first_open else ""}><summary><span>Spieltag {day}</span><small>{played} von {len(group)} gespielt</small></summary><div class="plan-list">')
            for f,result in group:
                if result:
                    v1,v2,mid,walkover,wo_winner=result
                    if walkover:
                        result_text='WO'
                        status='<b>Kampflos</b>'
                    else:
                        result_text=f'{v1} : {v2}'
                        status='<b>Gespielt</b>'
                else:
                    result_text='vs.'
                    status='<span class="plan-pending">Offen</span>'
                s.append(f'<div class="plan-fixture"><div class="plan-player plan-p1">{text(f["a"])}</div><div class="plan-result">{text(result_text)}</div><div class="plan-player plan-p2">{text(f["b"])}</div><div class="plan-state">{status}</div>')
                details=[]
                if f['date']: details.append(text(f['date']))
                if f['time']: details.append(text(f['time'])+' Uhr')
                if f['place']: details.append(text(f['place']))
                if f['note']: details.append(text(f['note']))
                if result and result[3] and result[4]: details.append('Kampflos für '+text(result[4]))
                if result and not result[3] and re.fullmatch(r'[0-9a-fA-F-]{36}',str(result[2])):
                    details.append(f'<a href="https://play.autodarts.com/history/matches/{text(result[2])}" target="_blank" rel="noopener noreferrer">Match ansehen ↗</a>')
                if details: s.append('<div class="plan-detail">'+' · '.join(details)+'</div>')
                s.append('</div>')
            s.append('</div></details>')
    else:
        s.append('<div class="plan-empty">Für diese Saison ist noch kein Spielplan hinterlegt. Sobald die Begegnungen eingetragen sind, erscheinen sie hier automatisch.</div>')
    s.extend(['</section>',SECTION_END])
    return ''.join(s)


def inject_page(path,content):
    markup=path.read_text(encoding='utf-8')
    markup=re.sub(re.escape(SECTION_START)+r'.*?'+re.escape(SECTION_END),'',markup,flags=re.S)
    markup=markup.replace(CSS,'')
    target='<!-- LIGA_SPIELPLAN_SLOT -->'
    if target not in markup or '</head>' not in markup:
        raise ValueError(f'{path.name}: Spielplan-Einfügestelle nicht gefunden, keine Änderungen geschrieben')
    markup=markup.replace('</head>',CSS+'</head>',1)
    markup=markup.replace(target,content,1)
    path.write_text(markup,encoding='utf-8')


def run(site,folder):
    schedule=read_schedule(folder)
    current,all_seasons=seasons(folder)
    all_matches=matches(folder)
    pages=sorted([p for p in site.glob('*.html') if p.name=='index.html' or p.name.startswith('season-')])
    if not pages: raise ValueError('Keine HTML-Seiten im site-Ordner gefunden')
    # Prüfen, dass jede Seite die richtige Struktur hat, bevor wir irgendetwas ändern.
    for page in pages:
        if '<!-- LIGA_SPIELPLAN_SLOT -->' not in page.read_text(encoding='utf-8'):
            raise ValueError(f'{page.name}: Spielplan-Einfügestelle fehlt')
    for page in pages:
        sid=current if page.name=='index.html' else page.stem.removeprefix('season-')
        sname=all_seasons.get(sid,'')
        effective=[r for r in schedule if r['season'] or (sid==current)]
        render_html=render(effective,all_matches,sid,sname,'')
        inject_page(page,render_html)
    print(f'Spielplan: {len(schedule)} eingetragene Begegnungen, {len(pages)} HTML-Seiten aktualisiert; Quelle: Liga-Daten/Spielplan.csv')


if __name__=='__main__':
    if len(sys.argv)!=3: sys.exit('Aufruf: insert-spielplan.py SITE_ORDNER LIGA_DATEN_ORDNER')
    try: run(Path(sys.argv[1]),Path(sys.argv[2]))
    except (ValueError, OSError, UnicodeError) as err: sys.exit('FEHLER SPIELPLAN: '+str(err))
