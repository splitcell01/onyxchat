import {
  createContext, useContext, useState, useCallback,
  useRef, useEffect, type ReactNode,
} from 'react'

import { useWebSocket }                              from '../hooks/useWebSocket'
import { fetchMessages, sendMessage as apiSendMessage } from '../api/messages'
import { fetchPublicKey }                            from '../api/keys'
import { useAuth }                                   from './AuthContext'
import {
  getOrCreateKeyPair,
  deriveSharedKey,
  encryptMessage,
  decryptMessage,
} from '../lib/crypto'

import type { Contact, Message, WSChatMessage, WSTyping, WSPresence, WSKeyChanged } from '../types'
import { fetchContacts } from '../api/contacts'

// ─── Types ────────────────────────────────────────────────────────────────────

interface ChatState {
  contacts:         Contact[]
  messages:         Record<string, Message[]>
  hasMore:          Record<string, boolean>
  activePeer:       Contact | null
  typing:           Record<string, boolean>
  unread:           Record<string, number>
  selectPeer:       (username: string) => void
  sendMessage:      (body: string) => Promise<void>
  sendTyping:       (isTyping: boolean) => void
  loadContacts:     () => Promise<void>
  loadMoreMessages: (username: string) => Promise<void>
}

const ChatContext = createContext<ChatState | null>(null)

// ─── Provider ─────────────────────────────────────────────────────────────────

export function ChatProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  const [contacts,   setContacts]   = useState<Contact[]>([])
  const [messages,   setMessages]   = useState<Record<string, Message[]>>({})
  const [hasMore,    setHasMore]    = useState<Record<string, boolean>>({})
  const [activePeer, setActivePeer] = useState<Contact | null>(null)
  const [typing,     setTyping]     = useState<Record<string, boolean>>({})
  const [unread,     setUnread]     = useState<Record<string, number>>({})

  const typingTimers    = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const activePeerRef   = useRef<Contact | null>(null)
  const lastMsgId       = useRef<Record<string, number>>({})
  const contactsRef     = useRef<Contact[]>([])
  // Optimistic IDs are negative so they never collide with positive server IDs.
  const optimisticIdSeq = useRef(-1)

  // Shared key cache: username → { key, publicKeyB64 }
  // We cache the peer's public key fingerprint alongside the derived key.
  // If the peer's public key on the server has changed, we re-derive.
  const sharedKeyCache = useRef<Map<string, { key: CryptoKey; pubKey: string }>>(new Map())

  activePeerRef.current = activePeer
  useEffect(() => { contactsRef.current = contacts }, [contacts])

  // When the user logs out and back in a new ECDH keypair is generated.
  // The sharedKeyCache holds AES keys derived from the *previous* private key,
  // so we must clear it whenever the user identity changes.
  useEffect(() => {
    sharedKeyCache.current.clear()
  }, [user])

  // ── Shared key helper ──────────────────────────────────────────────────────

  const getSharedKey = useCallback(async (peerUsername: string): Promise<CryptoKey | null> => {
    const theirPubKeyB64 = await fetchPublicKey(peerUsername)
    if (!theirPubKeyB64) return null

    // Return cached key only if the peer's public key hasn't changed
    const cached = sharedKeyCache.current.get(peerUsername)
    if (cached && cached.pubKey === theirPubKeyB64) return cached.key

    // Derive fresh shared key (peer rotated their keypair, or first time)
    const kp        = await getOrCreateKeyPair()
    const sharedKey = await deriveSharedKey(kp.privateKey, theirPubKeyB64)
    sharedKeyCache.current.set(peerUsername, { key: sharedKey, pubKey: theirPubKeyB64 })
    return sharedKey
  }, [])

  // ── Decrypt a single incoming message in-place ─────────────────────────────

  const tryDecrypt = useCallback(async (
    msg: Message,
    peerUsername: string,
  ): Promise<Message> => {
    if (!msg.encrypted || !msg.iv) return msg
    try {
      const sharedKey = await getSharedKey(peerUsername)
      if (!sharedKey) return { ...msg, body: '🔒 (no key)' }
      const plaintext = await decryptMessage(sharedKey, msg.body, msg.iv)
      return { ...msg, body: plaintext }
    } catch {
      return { ...msg, body: '🔒 Unable to decrypt' }
    }
  }, [getSharedKey])

  // ── WebSocket handlers ─────────────────────────────────────────────────────

  const onMessage = useCallback(async (event: WSChatMessage) => {
    const raw = event.message
    if (!raw || !user) return
    if (raw.senderId === user.id) return

    const peerUsername =
      contactsRef.current.find(c => c.id === raw.senderId)?.username
      ?? contactsRef.current.find(c => c.id === raw.recipientId)?.username

    if (!peerUsername) return

    const msg = await tryDecrypt(raw, peerUsername)

    setMessages(prev => {
      const existing = prev[peerUsername] ?? []
      if (existing.some(m => m.id === msg.id)) return prev
      return { ...prev, [peerUsername]: [...existing, msg] }
    })

    if (activePeerRef.current?.username !== peerUsername) {
      setUnread(prev => ({ ...prev, [peerUsername]: (prev[peerUsername] ?? 0) + 1 }))
    }
  }, [user, tryDecrypt])

  const onTyping = useCallback((msg: WSTyping) => {
    if (!activePeerRef.current || msg.from !== activePeerRef.current.username) return
    setTyping(prev => ({ ...prev, [msg.from]: msg.isTyping }))
    clearTimeout(typingTimers.current[msg.from])
    if (msg.isTyping) {
      typingTimers.current[msg.from] = setTimeout(
        () => setTyping(prev => ({ ...prev, [msg.from]: false })),
        3000,
      )
    }
  }, [])

  const onPresence = useCallback((msg: WSPresence) => {
    setContacts(prev => prev.map(c =>
      c.id === msg.userId ? { ...c, online: msg.status === 'online' } : c,
    ))
  }, [])

  const onKeyChanged = useCallback((msg: WSKeyChanged) => {
    // Evict the cached shared secret so it is re-derived with the new public key
    // on the next send or decrypt. The next call to getSharedKey will re-fetch
    // the peer's public key from the server and derive a fresh AES key.
    sharedKeyCache.current.delete(msg.username)
  }, [])

  const { send } = useWebSocket({ onMessage, onTyping, onPresence, onKeyChanged }, !!user)

  // ── Contacts ───────────────────────────────────────────────────────────────

  const loadContacts = useCallback(async () => {
    if (!user) return
    const contacts = await fetchContacts()
    setContacts(prev => {
      const prevMap = new Map(prev.map(c => [c.id, c]))
      return contacts
        .filter(u => u.id !== user.id)
        .map(u => ({ ...u, online: prevMap.get(u.id)?.online ?? false }))
    })
  }, [user])

  useEffect(() => { loadContacts() }, [loadContacts])

  // ── Select peer (load + decrypt history) ──────────────────────────────────

  const selectPeer = useCallback(async (username: string) => {
    const peer = contacts.find(c => c.username === username)
    if (!peer) return
    setActivePeer(peer)
    setUnread(prev => ({ ...prev, [username]: 0 }))

    if (!messages[username]) {
      const { messages: raw, hasMore: more } = await fetchMessages(username, 0)
      const decrypted = await Promise.all(raw.map(m => tryDecrypt(m, username)))
      setMessages(prev => ({ ...prev, [username]: decrypted }))
      setHasMore(prev => ({ ...prev, [username]: more }))
      if (decrypted.length) lastMsgId.current[username] = decrypted[decrypted.length - 1].id
    }
  }, [contacts, messages, tryDecrypt])

  // ── Load next page of history ─────────────────────────────────────────────

  const loadMoreMessages = useCallback(async (username: string) => {
    const sinceId = lastMsgId.current[username] ?? 0
    const { messages: raw, hasMore: more } = await fetchMessages(username, sinceId)
    if (!raw.length) return
    const decrypted = await Promise.all(raw.map(m => tryDecrypt(m, username)))
    setMessages(prev => {
      const existing = prev[username] ?? []
      // Guard against duplicates that may arrive via WS while we were fetching
      const existingIds = new Set(existing.map(m => m.id))
      const fresh = decrypted.filter(m => !existingIds.has(m.id))
      return fresh.length ? { ...prev, [username]: [...existing, ...fresh] } : prev
    })
    setHasMore(prev => ({ ...prev, [username]: more }))
    lastMsgId.current[username] = decrypted[decrypted.length - 1].id
  }, [tryDecrypt])

  // ── Send (encrypt → optimistic → confirm) ─────────────────────────────────

  const sendMessage = useCallback(async (plaintext: string) => {
    if (!activePeer || !user) return

    const clientMessageId = crypto.randomUUID()

    const optimistic: Message = {
      id:          optimisticIdSeq.current--,
      senderId:    user.id,
      recipientId: activePeer.id,
      body:        plaintext,
      createdAt:   new Date().toISOString(),
      encrypted:   false,
    }

    setMessages(prev => ({
      ...prev,
      [activePeer.username]: [...(prev[activePeer.username] ?? []), optimistic],
    }))

    try {
      let body      = plaintext
      let iv: string | undefined
      let encrypted = false

      const sharedKey = await getSharedKey(activePeer.username)
      if (sharedKey) {
        const payload = await encryptMessage(sharedKey, plaintext)
        body      = payload.body
        iv        = payload.iv
        encrypted = true
      }

      const saved = await apiSendMessage({
        recipientUsername: activePeer.username,
        body,
        iv,
        encrypted,
        clientMessageId,
      })

      // Replace optimistic entry with confirmed server message
      // Keep plaintext body in UI — don't show ciphertext to the sender
      setMessages(prev => {
        const arr = [...(prev[activePeer.username] ?? [])]
        const idx = arr.findIndex(m => m.id === optimistic.id)
        if (idx >= 0) arr[idx] = { ...saved, body: plaintext }
        return { ...prev, [activePeer.username]: arr }
      })
    } catch {
      // Mark optimistic message as failed so the user can see it didn't send
      setMessages(prev => {
        const arr = (prev[activePeer.username] ?? []).map(m =>
          m.id === optimistic.id ? { ...m, failed: true } : m,
        )
        return { ...prev, [activePeer.username]: arr }
      })
    }
  }, [activePeer, user, getSharedKey])

  // ── Typing ─────────────────────────────────────────────────────────────────

  const sendTyping = useCallback((isTyping: boolean) => {
    if (!activePeer) return
    send({ type: 'typing', to: activePeer.username, isTyping })
  }, [activePeer, send])

  return (
    <ChatContext.Provider value={{
      contacts, messages, hasMore, activePeer, typing, unread,
      selectPeer, sendMessage, sendTyping, loadContacts, loadMoreMessages,
    }}>
      {children}
    </ChatContext.Provider>
  )
}

export function useChat() {
  const ctx = useContext(ChatContext)
  if (!ctx) throw new Error('useChat must be used within ChatProvider')
  return ctx
}