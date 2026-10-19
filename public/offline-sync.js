/* Automatic refresh uses the public encryption key, never an unlock key. */
(() => {
  'use strict';
  let timer, busy = false;
  const status = document.getElementById('offline-sync-status');
  const banner = document.getElementById('offline-connection-status');
  async function connection() {
    banner.hidden = navigator.onLine || !currentUser;
    try { document.getElementById('offline-saved-link').hidden = !await GnotesOfflineStore.get(); } catch {}
  }
  function schedule() { clearTimeout(timer); timer = setTimeout(refresh, 1200); }
  async function refresh() {
    if (busy) { schedule(); return; }
    const owner = currentUser;
    if (!owner || document.hidden || !navigator.onLine || !crypto.subtle || !window.indexedDB) return;
    busy = true;
    try {
      const savedAt = await GnotesOfflineStore.refresh(owner.username);
      if (owner === currentUser) status.textContent = savedAt ? `Updated ${new Date(savedAt).toLocaleString()}` : '';
    } catch {
      if (owner === currentUser) status.textContent = 'Update unavailable · saved copy kept';
    } finally { busy = false; }
  }
  window.GnotesOfflineSync = { schedule };
  window.addEventListener('online', () => { connection(); schedule(); });
  window.addEventListener('offline', connection);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) schedule(); });
  setInterval(schedule, 60000);
  connection(); schedule();
})();
