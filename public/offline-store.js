/* Only ciphertext and nonsecret encryption parameters are persisted. */
(() => {
  'use strict';
  const database = 'gnotes-offline-v1';
  const revisionKey = 'gnotes-offline-revision';
  const channel = typeof BroadcastChannel === 'function' ? new BroadcastChannel('gnotes-offline') : null;
  const revision = () => localStorage.getItem(revisionKey) || '';
  function open() {
    return new Promise((resolve, reject) => {
      const request = indexedDB.open(database, 1);
      request.onupgradeneeded = () => request.result.createObjectStore('vault');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error('Close other gnotes tabs and try again.'));
    });
  }
  async function access(mode, action) {
    const db = await open();
    try {
      return await new Promise((resolve, reject) => {
        const tx = db.transaction('vault', mode);
        let result;
        const request = action(tx.objectStore('vault'));
        request.onsuccess = () => { result = request.result; };
        tx.oncomplete = () => resolve(result);
        tx.onabort = tx.onerror = () => reject(tx.error || new Error('Device storage is unavailable.'));
      });
    } finally { db.close(); }
  }
  async function key(passphrase, salt) {
    const material = await crypto.subtle.importKey('raw', new TextEncoder().encode(passphrase), 'PBKDF2', false, ['deriveKey']);
    return crypto.subtle.deriveKey({ name: 'PBKDF2', salt, iterations: 600000, hash: 'SHA-256' }, material,
      { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
  }
  async function encrypt(snapshot, passphrase) {
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const cipher = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await key(passphrase, salt),
      new TextEncoder().encode(JSON.stringify(snapshot)));
    return { version: 1, salt, iv, cipher };
  }
  async function decrypt(record, passphrase) {
    if (record?.version !== 1) throw new Error('Unsupported offline copy. Save a new copy while online.');
    const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: record.iv }, await key(passphrase, record.salt), record.cipher);
    const data = JSON.parse(new TextDecoder().decode(plain));
    if (data.version !== 1 || !Array.isArray(data.notes) || !Number.isInteger(data.owner)) throw new Error('Invalid offline copy.');
    return data;
  }
  async function save(record, expectedRevision) {
    const db = await open();
    try {
      if (revision() !== expectedRevision) throw new Error('Offline settings changed in another tab. Try again.');
      await new Promise((resolve, reject) => {
        const tx = db.transaction('vault', 'readwrite');
        tx.objectStore('vault').put(record, 'copy');
        tx.oncomplete = resolve;
        tx.onabort = tx.onerror = () => reject(tx.error);
      });
      channel?.postMessage('lock');
    } finally { db.close(); }
  }
  async function clear() {
    localStorage.setItem(revisionKey, crypto.randomUUID());
    channel?.postMessage('lock');
    await access('readwrite', store => store.clear());
  }
  window.GnotesOfflineStore = { get: () => access('readonly', store => store.get('copy')), encrypt, decrypt, save, clear, revision, channel };
})();
