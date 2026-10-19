/* Backup evidence is server-reported; unknown and failed checks stay explicit. */
window.GnotesBackupDashboard = (() => {
  let tabsEnabled = false, adminTabs = false, activeTab = 'overview';
  const tabList = document.getElementById('backup-tabs');
  function updateTabs() {
    tabList.hidden = !tabsEnabled;
    tabList.querySelectorAll('[data-backup-tab]').forEach(button => {
      const selected = button.dataset.backupTab === activeTab;
      button.hidden = !adminTabs && button.dataset.backupTab !== 'personal';
      button.setAttribute('aria-selected', String(selected));
      button.tabIndex = selected ? 0 : -1;
    });
    document.querySelectorAll('[data-backup-panel]').forEach(panel => { panel.hidden = tabsEnabled && panel.dataset.backupPanel !== activeTab; });
    const personal = document.getElementById('profile-backups');
    personal.hidden = tabsEnabled && activeTab !== 'personal';
    document.getElementById('site-backup-admin').hidden = !adminTabs || (tabsEnabled && activeTab === 'personal');
    if (tabsEnabled) {
      personal.setAttribute('role', 'tabpanel'); personal.setAttribute('aria-labelledby', 'backup-tab-personal'); personal.tabIndex = 0;
    } else {
      personal.removeAttribute('role'); personal.removeAttribute('aria-labelledby'); personal.removeAttribute('tabindex');
    }
  }
  function configureTabs(enabled, admin) {
    tabsEnabled = enabled; adminTabs = admin; activeTab = admin ? 'overview' : 'personal';
    updateTabs();
    document.getElementById('profile-panel').scrollTop = 0;
  }
  function selectTab(name, focus = false) {
    if (!tabsEnabled || (!adminTabs && name !== 'personal')) return;
    clearBackupSecrets();
    document.getElementById('profile-backup-password').value = '';
    activeTab = name;
    updateTabs();
    document.getElementById('profile-panel').scrollTop = 0;
    if (focus) document.getElementById('backup-tab-' + name).focus();
  }
  tabList.addEventListener('click', event => {
    const button = event.target.closest('[data-backup-tab]');
    if (button) selectTab(button.dataset.backupTab);
  });
  tabList.addEventListener('keydown', event => {
    if (!['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
    const buttons = [...tabList.querySelectorAll('[data-backup-tab]')].filter(button => !button.hidden);
    const current = buttons.findIndex(button => button === document.activeElement);
    if (current < 0) return;
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (current + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
    event.preventDefault(); selectTab(buttons[next].dataset.backupTab, true);
  });
  const date = value => value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : value || 'Not recorded';
  function card(name, headline, detail, tone = 'unknown') {
    const heading = document.getElementById(`backup-${name}-headline`);
    heading.textContent = headline;
    heading.parentElement.dataset.tone = tone;
    document.getElementById(`backup-${name}-detail`).textContent = detail;
  }
  function render(info) {
    const local = info.local;
    const latest = local?.latest;
    const latestTime = latest && Date.parse(latest.time);
    const fresh = latest && Number.isFinite(latestTime) && latestTime <= Date.now() + 300000 && Date.now() - latestTime <= 36*3600000;
    const scheduled = local?.schedule_enabled && local?.schedule_active;
    card('local', local ? latest ? fresh ? 'Recent backup available' : 'Latest backup is over 36 hours old' : 'No daily backup found' : 'Local status unavailable', local ? `${latest ? `${date(latest.time)} · ${local.count} retained daily backups. ` : ''}Daily schedule: ${scheduled ? 'enabled' : 'paused or not enabled'}. Next run: ${local.next_run || 'none scheduled'}. Last job: ${local.last_finished ? `${local.last_result || 'unknown'} · ${date(local.last_finished)}` : 'no completion recorded'}.` : 'The server backup service must support local backup reporting.', fresh && scheduled ? 'success' : 'attention');
    const cloudScheduled = info.schedule_enabled && info.schedule_active;
    const cloudLatest = info.snapshots?.[0];
    card('offsite', !info.configured ? 'Not configured on this server' : cloudScheduled ? 'Daily off-server backups enabled' : 'Off-server schedule paused', `${cloudLatest ? `Last listed snapshot: ${date(cloudLatest.time)} · ${cloudLatest.id}. ` : 'No off-server history loaded. '}${info.configured ? `Next run: ${info.next_run || 'none scheduled'}. Check the last job result below.` : 'Backups made from another computer are not tracked by this service.'}`, !info.configured || !cloudScheduled ? 'attention' : 'info');
    card('encryption', local?.encryption_required ? 'Database encryption required' : 'Encryption not confirmed', local?.encryption_required ? `The server is configured to reject startup without its database key. Key currently ${local.key_available ? 'available' : 'unavailable'}. Keep a separate offline copy; a login password cannot unlock a database backup.` : 'No encryption requirement was reported. A successful encrypted recovery test verifies the tested backup.', local?.encryption_required && local.key_available ? 'info' : 'attention');
    const checks = info.recovery_checks || {};
    const tested = Object.entries(checks).filter(([, value]) => value?.tested_at);
    card('recovery', tested.length ? 'Recovery checks recorded' : 'No recovery test recorded', tested.length ? tested.map(([kind,value]) => `${kind === 'local' ? 'Local' : 'Off-server'}: ${date(value.tested_at)} · ${value.archive}${value.snapshot ? ` · snapshot ${value.snapshot}` : ''}.`).join(' ') + ' These checks used the server key; your offline key copy still needs a separate test.' : 'Use Test local recovery or Test off-server recovery below. Neither changes the running database. Offline-key tests on another computer are not tracked here.', tested.length ? 'info' : 'attention');
    const history = document.getElementById('backup-local-history'); history.replaceChildren();
    for (const item of local?.backups || []) { const row = document.createElement('li'); row.textContent = `${date(item.time)} · ${item.name} · ${Number(item.bytes).toLocaleString()} bytes`; history.append(row); }
    if (!history.children.length) { const row = document.createElement('li'); row.textContent = local ? 'No local daily backups found.' : 'Local history unavailable.'; history.append(row); }
    document.getElementById('backup-checked-at').textContent = `Status checked ${new Date().toLocaleTimeString()}. Recovery tests refer to the named backup, not to newer notes.`;
  }
  function unavailable() {
    for (const name of ['local','offsite','encryption','recovery']) card(name, 'Status unavailable', 'Backup protection could not be checked. Refresh to retry.', 'unknown');
    document.getElementById('backup-checked-at').textContent = 'Could not refresh backup status. Previous results are not current confirmation.';
  }
  let commandCopyTimer;
  document.getElementById('backup-recovery-command-copy').addEventListener('click', async event => {
    const button = event.currentTarget;
    const command = document.getElementById('backup-recovery-command');
    const status = document.getElementById('backup-recovery-command-status');
    const label = button.querySelector('span');
    clearTimeout(commandCopyTimer);
    status.textContent = '';
    let copied = false;
    try {
      await navigator.clipboard.writeText(command.textContent.trim());
      copied = true;
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(command);
      selection.removeAllRanges(); selection.addRange(range);
      try { copied = document.execCommand('copy'); } catch {}
    }
    label.textContent = copied ? 'Copied' : 'Copy command';
    status.textContent = copied ? 'Recovery command copied.' : 'Command selected. Use your browser’s Copy action.';
    commandCopyTimer = setTimeout(() => { label.textContent = 'Copy command'; status.textContent = ''; }, 3000);
  });
  let personalRevision = 0;
  function resetPersonal() {
    personalRevision++;
    document.getElementById('personal-recovery-test-form').reset();
    document.getElementById('personal-recovery-result').textContent = '';
    document.querySelector('#personal-recovery-test-form button').disabled = false;
  }
  function reset() { unavailable(); resetPersonal(); }
  document.getElementById('personal-recovery-test-form').addEventListener('submit', async event => {
    event.preventDefault();
    const revision = ++personalRevision;
    const owner = currentUser;
    const status = document.getElementById('personal-recovery-result');
    const button = event.currentTarget.querySelector('button');
    const backup = document.getElementById('personal-recovery-backup').files[0];
    const key = document.getElementById('personal-recovery-key').files[0];
    if (!backup || !key) { status.textContent = 'Choose both files.'; return; }
    button.disabled = true; status.textContent = 'Testing recovery in this browser…';
    try {
      if (backup.size > 96 * 1024 * 1024) throw new Error('Backup is too large for this browser test.');
      const plain = await decryptPersonalBackup(backup, key);
      const data = JSON.parse(await plain.text());
      if (data.format !== 'gnotes' || ![1,2,3,4].includes(data.version) || !Array.isArray(data.notes) || !data.notes.every(note => typeof note.content === 'string' && typeof note.title === 'string')) throw new Error('The decrypted file is not a supported note backup.');
      if (currentUser !== owner || revision !== personalRevision) return;
      status.textContent = `Recovery test passed: ${data.notes.length} note${data.notes.length === 1 ? '' : 's'} unlocked. Exported ${date(data.exported_at)}. No notes imported; files stayed in this browser. Use Export & import to restore when needed.`;
    } catch (error) { if (currentUser === owner && revision === personalRevision) status.textContent = `Recovery test failed: ${error.message || 'could not read the backup'}`; }
    finally {
      if (revision === personalRevision) {
        button.disabled = false;
        document.getElementById('personal-recovery-backup').value = '';
        document.getElementById('personal-recovery-key').value = '';
      }
    }
  });
  return {render, unavailable, reset, resetPersonal, configureTabs, updateTabs, selectTab, get activeTab() { return activeTab; }};
})();
