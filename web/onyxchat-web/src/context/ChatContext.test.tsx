// @vitest-environment jsdom

import { vi, describe, it, expect, beforeAll, beforeEach, afterEach } from 'vitest'
import { render, act, cleanup } from '@testing-library/react'
import { ChatProvider, useChat } from './ChatContext'
import type { Message } from '../types'

// ─── Module mocks ─────────────────────────────────────────────────────────────

// useAuth must return a stable object reference — a new object every call would
// invalidate the loadContacts useCallback on every render, causing an infinite loop.
vi.mock('./AuthContext', () => ({
  useAuth: vi.fn(),
}))

// Use vi.fn() so E2E tests can pull the captured onMessage handler from mock.calls
vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: vi.fn().mockImplementation(() => ({ send: vi.fn() })),
}))

vi.mock('../api/contacts', () => ({
  fetchContacts: vi.fn().mockResolvedValue([
    { id: 2, username: 'bob', online: false },
  ]),
}))

vi.mock('../api/messages', () => ({
  fetchMessages: vi.fn().mockResolvedValue({ messages: [], hasMore: false }),
  sendMessage:   vi.fn(),
}))

// No E2E key → getSharedKey returns null → messages sent as plaintext
vi.mock('../api/keys', () => ({
  fetchPublicKey: vi.fn().mockResolvedValue(null),
}))

vi.mock('../lib/crypto', () => ({
  getOrCreateKeyPair: vi.fn(),
  deriveSharedKey:    vi.fn(),
  encryptMessage:     vi.fn(),
  decryptMessage:     vi.fn(),
}))

import * as messagesApi from '../api/messages'
import * as AuthContext  from './AuthContext'
import * as cryptoLib    from '../lib/crypto'
import * as keysApi      from '../api/keys'
import * as wsHook       from '../hooks/useWebSocket'
import type { WSChatMessage } from '../types'

// Stable auth state — same object reference across all renders.
// Must satisfy the full AuthState interface or TS will complain.
const MOCK_AUTH = {
  user:            { id: 1, username: 'alice' },
  isAuthenticated: true,
  login:           vi.fn(),
  register:        vi.fn(),
  logout:          vi.fn(),
}
vi.mocked(AuthContext.useAuth).mockReturnValue(MOCK_AUTH)

// ─── Test harness ─────────────────────────────────────────────────────────────

type ChatHandle = ReturnType<typeof useChat>
let chat!: ChatHandle

// Capture component keeps chat up-to-date as the context re-renders
function Capture() {
  chat = useChat()
  return null
}

async function setup() {
  await act(async () => {
    render(
      <ChatProvider>
        <Capture />
      </ChatProvider>
    )
  })
  // Select bob so activePeer is set and messages['bob'] is initialised
  await act(async () => {
    chat.selectPeer('bob')
  })
}

// ─── sendMessage – failure UX ─────────────────────────────────────────────────

describe('sendMessage – failure', () => {
  afterEach(() => cleanup())

  beforeEach(async () => {
    vi.clearAllMocks()
    vi.mocked(messagesApi.fetchMessages).mockResolvedValue({ messages: [], hasMore: false })
    await setup()
  })

  it('keeps the message in the list marked as failed', async () => {
    vi.mocked(messagesApi.sendMessage).mockRejectedValue(new Error('network'))

    await act(async () => {
      await chat.sendMessage('hello bob')
    })

    const msgs = chat.messages['bob']
    expect(msgs).toHaveLength(1)
    expect(msgs[0].failed).toBe(true)
  })

  it('preserves the original message body after failure', async () => {
    vi.mocked(messagesApi.sendMessage).mockRejectedValue(new Error('timeout'))

    await act(async () => {
      await chat.sendMessage('important message')
    })

    expect(chat.messages['bob'][0].body).toBe('important message')
  })

  it('does not mark the message failed on success', async () => {
    const serverMsg: Message = {
      id: 999, senderId: 1, recipientId: 2,
      body: 'hi', createdAt: new Date().toISOString(), encrypted: false,
    }
    vi.mocked(messagesApi.sendMessage).mockResolvedValue(serverMsg)

    await act(async () => {
      await chat.sendMessage('hi')
    })

    expect(chat.messages['bob'][0].failed).toBeFalsy()
  })
})

// ─── sendMessage – optimistic ID uniqueness ────────────────────────────────────

describe('sendMessage – optimistic IDs', () => {
  afterEach(() => cleanup())

  beforeEach(async () => {
    vi.clearAllMocks()
    vi.mocked(messagesApi.fetchMessages).mockResolvedValue({ messages: [], hasMore: false })
    // Both sends fail so both optimistic messages remain in the list
    vi.mocked(messagesApi.sendMessage).mockRejectedValue(new Error('network'))
    await setup()
  })

  it('assigns unique IDs when messages are sent in rapid succession', async () => {
    await act(async () => {
      await Promise.all([
        chat.sendMessage('first'),
        chat.sendMessage('second'),
      ])
    })

    const msgs = chat.messages['bob']
    expect(msgs).toHaveLength(2)
    expect(msgs[0].id).not.toBe(msgs[1].id)
  })

  it('uses negative IDs for optimistic messages so server IDs never collide', async () => {
    await act(async () => {
      await chat.sendMessage('test')
    })

    expect(chat.messages['bob'][0].id).toBeLessThan(0)
  })
})

// ─── sendMessage – success ────────────────────────────────────────────────────

describe('sendMessage – success', () => {
  afterEach(() => cleanup())

  beforeEach(async () => {
    vi.clearAllMocks()
    vi.mocked(messagesApi.fetchMessages).mockResolvedValue({ messages: [], hasMore: false })
    await setup()
  })

  it('replaces the optimistic entry with the server-confirmed message', async () => {
    const serverMsg: Message = {
      id: 999, senderId: 1, recipientId: 2,
      body: 'confirmed', createdAt: new Date().toISOString(), encrypted: false,
    }
    vi.mocked(messagesApi.sendMessage).mockResolvedValue(serverMsg)

    await act(async () => {
      await chat.sendMessage('hello')
    })

    const msgs = chat.messages['bob']
    expect(msgs).toHaveLength(1)
    expect(msgs[0].id).toBe(999)
    expect(msgs[0].body).toBe('hello') // plaintext preserved in UI, not ciphertext
  })

  it('ends up with exactly one message per send (no duplicates)', async () => {
    const serverMsg: Message = {
      id: 1001, senderId: 1, recipientId: 2,
      body: 'hi', createdAt: new Date().toISOString(), encrypted: false,
    }
    vi.mocked(messagesApi.sendMessage).mockResolvedValue(serverMsg)

    await act(async () => {
      await chat.sendMessage('hi')
    })

    expect(chat.messages['bob']).toHaveLength(1)
  })
})

// ─── E2E encrypted path ───────────────────────────────────────────────────────
//
// These tests use real ECDH keypairs and the actual crypto implementations.
// No plaintext ever reaches the mock API; the context layer must encrypt on
// send and decrypt on receive.

describe('E2E encrypted path', () => {
  let aliceKp:       CryptoKeyPair
  let bobKp:         CryptoKeyPair
  let alicePubKeyB64: string
  let bobPubKeyB64:   string
  let realCrypto:    typeof import('../lib/crypto')

  beforeAll(async () => {
    realCrypto = await vi.importActual('../lib/crypto')

    const params = { name: 'ECDH', namedCurve: 'P-256' } as const
    aliceKp = await crypto.subtle.generateKey(params, true, ['deriveKey'])
    bobKp   = await crypto.subtle.generateKey(params, true, ['deriveKey'])

    const exportB64 = async (key: CryptoKey) => {
      const raw = await crypto.subtle.exportKey('spki', key)
      return btoa(String.fromCharCode(...new Uint8Array(raw)))
    }
    alicePubKeyB64 = await exportB64(aliceKp.publicKey)
    bobPubKeyB64   = await exportB64(bobKp.publicKey)
  })

  afterEach(() => cleanup())

  beforeEach(async () => {
    vi.clearAllMocks()

    // Bob has uploaded his real public key
    vi.mocked(keysApi.fetchPublicKey).mockResolvedValue(bobPubKeyB64)
    vi.mocked(messagesApi.fetchMessages).mockResolvedValue({ messages: [], hasMore: false })

    // Alice's local keypair is the real one generated above
    vi.mocked(cryptoLib.getOrCreateKeyPair).mockResolvedValue(aliceKp)

    // Route through real crypto implementations
    vi.mocked(cryptoLib.deriveSharedKey).mockImplementation(realCrypto.deriveSharedKey)
    vi.mocked(cryptoLib.encryptMessage).mockImplementation(realCrypto.encryptMessage)
    vi.mocked(cryptoLib.decryptMessage).mockImplementation(realCrypto.decryptMessage)

    await setup()
  })

  it('sends ciphertext to the API, not the plaintext body', async () => {
    const serverMsg: Message = {
      id: 42, senderId: 1, recipientId: 2,
      body: '<server-ciphertext>', createdAt: new Date().toISOString(), encrypted: true,
    }
    vi.mocked(messagesApi.sendMessage).mockResolvedValue(serverMsg)

    await act(async () => { await chat.sendMessage('hello bob') })

    const [req] = vi.mocked(messagesApi.sendMessage).mock.calls[0]
    expect(req.encrypted).toBe(true)
    expect(req.iv).toBeDefined()
    expect(req.body).not.toBe('hello bob')
  })

  it('shows plaintext in the UI even though the server received ciphertext', async () => {
    const serverMsg: Message = {
      id: 42, senderId: 1, recipientId: 2,
      body: '<server-ciphertext>', createdAt: new Date().toISOString(), encrypted: true,
    }
    vi.mocked(messagesApi.sendMessage).mockResolvedValue(serverMsg)

    await act(async () => { await chat.sendMessage('hello bob') })

    expect(chat.messages['bob'][0].body).toBe('hello bob')
  })

  it('decrypts an incoming encrypted message from bob', async () => {
    // Bob derives shared key from his side and encrypts a message to alice
    const bobSharedKey  = await realCrypto.deriveSharedKey(bobKp.privateKey, alicePubKeyB64)
    const { body, iv }  = await realCrypto.encryptMessage(bobSharedKey, 'hey alice!')

    // Pull the live onMessage handler from the last useWebSocket call
    const { onMessage } = vi.mocked(wsHook.useWebSocket).mock.calls.at(-1)![0]

    await act(async () => {
      await (onMessage as (e: WSChatMessage) => Promise<void>)({
        type: 'message',
        message: { id: 99, senderId: 2, recipientId: 1, body, iv, encrypted: true,
                   createdAt: new Date().toISOString() },
      })
    })

    expect(chat.messages['bob']).toHaveLength(1)
    expect(chat.messages['bob'][0].body).toBe('hey alice!')
  })

  it('round-trip: what alice encrypts, bob can decrypt with the same ECDH key agreement', async () => {
    // Capture what the context actually sends to the API
    let sentBody = '', sentIv = ''
    vi.mocked(messagesApi.sendMessage).mockImplementation(async req => {
      sentBody = req.body
      sentIv   = req.iv!
      return { id: 43, senderId: 1, recipientId: 2, body: req.body,
               createdAt: new Date().toISOString(), encrypted: true }
    })

    await act(async () => { await chat.sendMessage('round trip works') })

    // Bob independently derives the shared key and decrypts the ciphertext
    const bobSharedKey = await realCrypto.deriveSharedKey(bobKp.privateKey, alicePubKeyB64)
    const decrypted    = await realCrypto.decryptMessage(bobSharedKey, sentBody, sentIv)
    expect(decrypted).toBe('round trip works')
  })
})
