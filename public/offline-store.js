/* Persist ciphertext, wrapped keys and public encryption parameters only. */
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
  async function encrypt(snapshot, passphrase, automatic = false) {
    if (automatic) {
      const pair = await crypto.subtle.generateKey({ name: 'RSA-OAEP', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-256' }, true, ['encrypt', 'decrypt']);
      const salt = crypto.getRandomValues(new Uint8Array(16));
      const privateIV = crypto.getRandomValues(new Uint8Array(12));
      const wrappedPrivate = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: privateIV }, await key(passphrase, salt), await crypto.subtle.exportKey('pkcs8', pair.privateKey));
      return seal(snapshot, { version: 2, owner: snapshot.owner, username: snapshot.username, publicKey: await crypto.subtle.exportKey('jwk', pair.publicKey), salt, privateIV, wrappedPrivate });
    }
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const cipher = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await key(passphrase, salt),
      new TextEncoder().encode(JSON.stringify(snapshot)));
    return { version: 1, salt, iv, cipher };
  }
  // Anyone with the public key can update a copy; reading still needs the
  // passphrase-protected private key. No unlock key is stored on the device.
  async function seal(snapshot, record) {
    const dataKey = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, true, ['encrypt']);
    const publicKey = await crypto.subtle.importKey('jwk', record.publicKey, { name: 'RSA-OAEP', hash: 'SHA-256' }, false, ['encrypt']);
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const cipher = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, dataKey, new TextEncoder().encode(JSON.stringify(snapshot)));
    const wrappedKey = await crypto.subtle.encrypt({ name: 'RSA-OAEP' }, publicKey, await crypto.subtle.exportKey('raw', dataKey));
    return { ...record, iv, cipher, wrappedKey, savedAt: snapshot.saved_at };
  }
  async function decrypt(record, passphrase) {
    let decryptKey;
    if (record?.version === 2) {
      const privateBytes = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: record.privateIV }, await key(passphrase, record.salt), record.wrappedPrivate);
      const privateKey = await crypto.subtle.importKey('pkcs8', privateBytes, { name: 'RSA-OAEP', hash: 'SHA-256' }, false, ['decrypt']);
      const dataKey = await crypto.subtle.decrypt({ name: 'RSA-OAEP' }, privateKey, record.wrappedKey);
      decryptKey = await crypto.subtle.importKey('raw', dataKey, { name: 'AES-GCM' }, false, ['decrypt']);
    } else if (record?.version === 1) {
      decryptKey = await key(passphrase, record.salt);
    } else throw new Error('Unsupported offline copy. Save a new copy while online.');
    const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: record.iv }, decryptKey, record.cipher);
    const data = JSON.parse(new TextDecoder().decode(plain));
    if (data.version !== 1 || !Array.isArray(data.notes) || !Number.isInteger(data.owner)
      || record.version === 2 && (data.owner !== record.owner || data.username !== record.username)) throw new Error('Invalid offline copy.');
    return data;
  }
  async function refresh(username) {
    const action = async () => {
      const expectedRevision = revision();
      const record = await access('readonly', store => store.get('copy'));
      if (record?.version !== 2 || record.username !== username) return null;
      const response = await fetch('/api/notes/offline', { cache: 'no-store' });
      if (!response.ok) throw new Error('Could not refresh offline notes. Your saved copy is unchanged.');
      const snapshot = await response.json();
      if (snapshot.owner !== record.owner || snapshot.username !== username || !Array.isArray(snapshot.notes)) throw new Error('Offline account changed.');
      const encrypted = await seal(snapshot, record);
      const session = await fetch('/api/auth/me', { cache: 'no-store' });
      if (!session.ok || (await session.json()).username !== username) throw new Error('Offline account changed.');
      await save(encrypted, expectedRevision, record.cipher);
      return snapshot.saved_at;
    };
    return navigator.locks ? navigator.locks.request('gnotes-offline-refresh', action) : action();
  }
  async function save(record, expectedRevision, expectedCipher) {
    const db = await open();
    try {
      if (revision() !== expectedRevision) throw new Error('Offline settings changed in another tab. Try again.');
      await new Promise((resolve, reject) => {
        const tx = db.transaction('vault', 'readwrite');
        const vault = tx.objectStore('vault');
        if (expectedCipher) {
          const read = vault.get('copy');
          read.onsuccess = () => {
            const current = read.result?.cipher;
            const expected = new Uint8Array(expectedCipher);
            if (!current || current.byteLength !== expected.length || !new Uint8Array(current).every((byte, i) => byte === expected[i]) || revision() !== expectedRevision) { tx.abort(); return; }
            vault.put(record, 'copy');
          };
        } else vault.put(record, 'copy');
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
  window.GnotesOfflineStore = { get: () => access('readonly', store => store.get('copy')), encrypt, decrypt, save, clear, revision, channel, refresh };
})();
