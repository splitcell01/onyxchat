/**
 * lib/crypto.ts
 * ECDH P-256 + AES-256-GCM end-to-end encryption.
 * No external dependencies — uses the browser's built-in Web Crypto API.
 */

// ─── IndexedDB key storage ────────────────────────────────────────────────────
// Private keys are stored as non-extractable CryptoKey objects directly in IDB.
// They never appear as raw bytes in JS memory.

const DB_NAME    = 'onyxchat_e2e';
const DB_VERSION = 2;
const STORE      = 'keypairs';
const KEY_ID     = 'local';

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = e => {
      const db = (e.target as IDBOpenDBRequest).result;
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'id' });
      }
    };
    req.onsuccess = e => resolve((e.target as IDBOpenDBRequest).result);
    req.onerror   = ()  => reject(req.error);
  });
}

async function idbGet(db: IDBDatabase): Promise<CryptoKeyPair | null> {
  return new Promise((resolve, reject) => {
    const req = db.transaction(STORE, 'readonly').objectStore(STORE).get(KEY_ID);
    req.onsuccess = () => {
      const r = req.result;
      resolve(r ? { publicKey: r.publicKey, privateKey: r.privateKey } : null);
    };
    req.onerror = () => reject(req.error);
  });
}

async function idbPut(db: IDBDatabase, kp: CryptoKeyPair): Promise<void> {
  return new Promise((resolve, reject) => {
    const req = db.transaction(STORE, 'readwrite').objectStore(STORE)
      .put({ id: KEY_ID, publicKey: kp.publicKey, privateKey: kp.privateKey });
    req.onsuccess = () => resolve();
    req.onerror   = () => reject(req.error);
  });
}

async function idbDelete(db: IDBDatabase): Promise<void> {
  return new Promise((resolve, reject) => {
    const req = db.transaction(STORE, 'readwrite').objectStore(STORE).delete(KEY_ID);
    req.onsuccess = () => resolve();
    req.onerror   = () => reject(req.error);
  });
}

// ─── Key lifecycle ────────────────────────────────────────────────────────────

/** Returns the stored keypair, generating + persisting a new one if absent. */
export async function getOrCreateKeyPair(): Promise<CryptoKeyPair> {
  const db       = await openDB();
  const existing = await idbGet(db);
  if (existing) return existing;

  const kp = await crypto.subtle.generateKey(
    { name: 'ECDH', namedCurve: 'P-256' },
    false,           // private key non-extractable — never leaves IDB
    ['deriveKey'],
  );

  await idbPut(db, kp);
  return kp;
}

/**
 * Exports the public key as a base64-encoded SPKI string.
 * Safe to send to the server — contains no private material.
 */
export async function exportPublicKey(kp: CryptoKeyPair): Promise<string> {
  const raw = await crypto.subtle.exportKey('spki', kp.publicKey);
  return bufToBase64(raw);
}

/** Wipes the stored keypair from IDB. Call on logout. */
export async function clearKeyPair(): Promise<void> {
  const db = await openDB();
  await idbDelete(db);
}

// ─── ECDH key agreement ───────────────────────────────────────────────────────

/**
 * Derives a shared AES-256-GCM key from your private key + the peer's
 * base64 SPKI public key. Both sides independently derive the same key
 * because ECDH is commutative.
 */
export async function deriveSharedKey(
  myPrivateKey: CryptoKey,
  theirPublicKeyB64: string,
): Promise<CryptoKey> {
  const theirKey = await crypto.subtle.importKey(
    'spki',
    base64ToBuf(theirPublicKeyB64),
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    [],
  );

  return crypto.subtle.deriveKey(
    { name: 'ECDH', public: theirKey },
    myPrivateKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

// ─── Encrypt / Decrypt ────────────────────────────────────────────────────────

export interface EncryptedPayload {
  body: string; // base64 AES-256-GCM ciphertext
  iv:   string; // base64 12-byte GCM nonce
}

export async function encryptMessage(
  sharedKey: CryptoKey,
  plaintext: string,
): Promise<EncryptedPayload> {
  const iv         = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv },
    sharedKey,
    new TextEncoder().encode(plaintext),
  );

  return { body: bufToBase64(ciphertext), iv: bufToBase64(iv.buffer) };
}

export async function decryptMessage(
  sharedKey: CryptoKey,
  body: string,
  iv: string,
): Promise<string> {
  const plain = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: base64ToBuf(iv) },
    sharedKey,
    base64ToBuf(body),
  );
  return new TextDecoder().decode(plain);
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function bufToBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf)
    let binary = ''
    for (let i = 0; i < bytes.byteLength; i++) {
        binary += String.fromCharCode(bytes[i])
    }
    return btoa(binary)
}

function base64ToBuf(b64: string): ArrayBuffer {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}