// Autodarts Liga v0.9.6 – fast, silent, same-origin revision polling.
(() => {
  'use strict';
  const meta = document.querySelector('meta[name="liga-data-revision"]');
  if (!meta) return;
  const currentRevision = meta.getAttribute('content');
  if (!currentRevision) return;
  let busy = false;
  const storageKey = 'autodarts-liga-scroll:' + location.pathname;
  try {
    const pos = sessionStorage.getItem(storageKey);
    if (pos !== null) { sessionStorage.removeItem(storageKey); window.scrollTo(0, Number(pos) || 0); }
  } catch (_) {}
  async function checkRevision() {
    if (busy || document.hidden) return;
    busy = true;
    try {
      const revisionUrl = new URL('revision.txt', location.href);
      revisionUrl.searchParams.set('check', String(Date.now()));
      const response = await fetch(revisionUrl, {cache: 'no-store', credentials: 'same-origin'});
      if (!response.ok) return;
      const fresh = (await response.text()).trim();
      if (/^[a-f0-9]{32}$/i.test(fresh) && fresh !== currentRevision) {
        try { sessionStorage.setItem(storageKey, String(window.scrollY)); } catch (_) {}
        window.location.reload();
      }
    } catch (_) { /* No disruption when offline */ }
    finally { busy = false; }
  }
  setInterval(checkRevision, 60_000);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) checkRevision(); });
})();
