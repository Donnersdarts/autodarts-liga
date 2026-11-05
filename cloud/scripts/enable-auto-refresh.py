#!/usr/bin/env python3
"""Enable lightweight auto refresh on the generated GitHub Pages HTML.
The revision ID changes only when the remote league data changes.
"""
import hashlib
import html
import pathlib
import sys


def revision_for(data_folder):
    folder = pathlib.Path(data_folder)
    files = sorted(p for p in folder.rglob('*.json') if p.is_file())
    schedule = folder / 'Spielplan.csv'
    if schedule.is_file():
        files.append(schedule)
        files.sort()
    if not files:
        raise SystemExit('FEHLER: Keine Match-/Saison-JSON-Dateien gefunden')
    digest = hashlib.sha256()
    for path in files:
        digest.update(str(path.relative_to(folder)).encode('utf-8'))
        digest.update(b'\x00')
        digest.update(path.read_bytes())
        digest.update(b'\x00')
    return digest.hexdigest()[:32]


def inject(site_folder, data_folder):
    site = pathlib.Path(site_folder)
    rid = revision_for(data_folder)
    pages = sorted(site.glob('*.html'))
    if not pages:
        raise SystemExit('FEHLER: Keine HTML-Seiten im Ausgabeordner')
    for page in pages:
        markup = page.read_text(encoding='utf-8')
        marker = f'<meta name="liga-data-revision" content="{html.escape(rid)}">'
        if '</head>' not in markup or '</body>' not in markup:
            raise SystemExit(f'FEHLER: Seite ohne HTML-Grundstruktur: {page.name}')
        # Idempotent if a build runs more than once on the same directory.
        import re
        markup = re.sub(r'<meta name="liga-data-revision" content="[^"]*">', '', markup)
        markup = markup.replace('</head>', marker + '</head>', 1)
        if '<script defer src="auto-refresh.js"></script>' not in markup:
            markup = markup.replace('</body>', '<script defer src="auto-refresh.js"></script></body>', 1)
        page.write_text(markup, encoding='utf-8')
    (site / 'revision.txt').write_text(rid + '\n', encoding='ascii')
    print(f'Auto-Refresh aktiv (60s): {len(pages)} Seiten, Revision {rid}')


if __name__ == '__main__':
    if len(sys.argv) != 3:
        sys.exit('Aufruf: enable-auto-refresh.py SITE_ORDNER LIGA_DATEN_ORDNER')
    inject(sys.argv[1], sys.argv[2])
