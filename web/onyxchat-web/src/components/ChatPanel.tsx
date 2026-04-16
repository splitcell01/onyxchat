// components/ChatPanel.tsx — replaces existing file
// Changes from original: lock badge in header when peer has E2E key,
// 🔒 icon on individual encrypted messages. All logic stays in context.

import { useEffect, useRef, useState, useCallback } from 'react'
import { useChat }  from '../context/ChatContext'
import { useAuth }  from '../context/AuthContext'
import { fetchPublicKey } from '../api/keys'

const initials = (name: string) => name.slice(0, 2).toUpperCase()

function formatTime(iso: string) {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export function ChatPanel() {
  const { user }                                                          = useAuth()
  const { activePeer, contacts, messages, hasMore, typing, sendMessage, retryMessage, sendTyping, loadMoreMessages } = useChat()
  const livePeer = contacts.find(c => c.id === activePeer?.id)
  const [input, setInput]                                                 = useState('')
  const [peerHasKey, setPeerHasKey]                                       = useState(false)
  const [loadingMore, setLoadingMore]                                     = useState(false)
  const messagesEndRef                                                    = useRef<HTMLDivElement>(null)
  const typingTimeout                                                     = useRef<ReturnType<typeof setTimeout> | null>(null)

  const msgs = activePeer ? (messages[activePeer.username] ?? []) : []

  // Check whether the active peer has uploaded an E2E key
  useEffect(() => {
    if (!activePeer) { setPeerHasKey(false); return }
    fetchPublicKey(activePeer.username).then(k => setPeerHasKey(!!k))
  }, [activePeer?.username])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [msgs])

  const handleLoadMore = useCallback(async () => {
    if (!activePeer || loadingMore) return
    setLoadingMore(true)
    try {
      await loadMoreMessages(activePeer.username)
    } finally {
      setLoadingMore(false)
    }
  }, [activePeer, loadingMore, loadMoreMessages])

  const handleSend = useCallback(async () => {
    const body = input.trim()
    if (!body || !activePeer) return
    setInput('')
    await sendMessage(body)
  }, [input, activePeer, sendMessage])

  const handleTyping = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value)
    sendTyping(true)
    if (typingTimeout.current) clearTimeout(typingTimeout.current)
    typingTimeout.current = setTimeout(() => sendTyping(false), 1500)
  }, [sendTyping])

  if (!activePeer) {
    return (
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg)' }}>
        <div style={{ textAlign: 'center', color: 'var(--text-mute)' }}>
          <svg width="64" height="64" viewBox="0 0 64 64" fill="none" style={{ margin: '0 auto 16px', display: 'block', opacity: 0.2 }}>
            <circle cx="32" cy="32" r="20" stroke="#2563eb" strokeWidth="3"/>
            <circle cx="32" cy="32" r="10" fill="#2563eb" opacity="0.5"/>
            <circle cx="32" cy="32" r="4" fill="#2563eb"/>
          </svg>
          <div style={{ fontSize: '18px', fontWeight: 600, color: 'var(--text)', marginBottom: '8px' }}>OnyxChat</div>
          <div style={{ fontSize: '13px', lineHeight: 1.6 }}>Select a contact to start a conversation.</div>
        </div>
      </div>
    )
  }

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, background: 'var(--bg)' }}>
      {/* Header */}
      <div style={{
        height: '56px', display: 'flex', alignItems: 'center',
        padding: '0 16px', borderBottom: '1px solid var(--border)', gap: '10px',
      }}>
        <div style={{
          width: '32px', height: '32px', borderRadius: '50%',
          background: 'var(--surface)', border: '1px solid var(--border-2)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: '12px', fontWeight: 600, color: 'var(--text-dim)',
        }}>
          {initials(activePeer.username)}
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '6px' }}>
            {activePeer.username}
            {/* Lock badge — shown when both sides have E2E keys */}
            {peerHasKey && (
              <span title="End-to-end encrypted" style={{ fontSize: '11px', color: 'var(--green, #22c55e)' }}>
                🔒
              </span>
            )}
          </div>
          <div style={{ fontSize: '12px', color: livePeer?.online ? 'var(--green)' : 'var(--text-mute)' }}>
            {livePeer?.online ? 'Online' : 'Offline'}
            {peerHasKey && <span style={{ marginLeft: '6px', opacity: 0.6 }}>· E2E encrypted</span>}
          </div>
        </div>
      </div>

      {/* Messages */}
      <div style={{ flex: 1, overflowY: 'auto', padding: '20px 16px', display: 'flex', flexDirection: 'column', gap: '4px' }}>
        {msgs.length === 0 ? (
          <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-mute)', fontSize: '13px', textAlign: 'center' }}>
            No messages yet. Say hello!
          </div>
        ) : (
          msgs.map((msg) => {
            const isMe = msg.senderId === user?.id
            return (
              <div key={msg.id} style={{ display: 'flex', justifyContent: isMe ? 'flex-end' : 'flex-start', gap: '8px', alignItems: 'flex-end' }}>
                {!isMe && (
                  <div style={{
                    width: '26px', height: '26px', borderRadius: '50%',
                    background: 'var(--surface)', border: '1px solid var(--border-2)',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: '10px', fontWeight: 600, color: 'var(--text-dim)', flexShrink: 0,
                  }}>
                    {initials(activePeer.username)}
                  </div>
                )}
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: isMe ? 'flex-end' : 'flex-start', maxWidth: 'min(68%, 420px)', gap: '2px' }}>
                  <div
                    onClick={msg.failed ? () => retryMessage(msg.id, msg.body) : undefined}
                    style={{
                      padding: '8px 12px',
                      borderRadius: isMe ? '20px 20px 4px 20px' : '20px 20px 20px 4px',
                      fontSize: '13.5px', lineHeight: 1.5, wordBreak: 'break-word', whiteSpace: 'pre-wrap',
                      background: msg.failed
                        ? 'rgba(239,68,68,0.12)'
                        : isMe ? 'linear-gradient(145deg, var(--blue), var(--blue-dim))' : 'var(--surface)',
                      border: msg.failed
                        ? '1px solid rgba(239,68,68,0.4)'
                        : isMe ? 'none' : '1px solid var(--border-2)',
                      color: msg.failed ? 'var(--red, #ef4444)' : isMe ? 'white' : 'var(--text)',
                      opacity: msg.failed ? 0.85 : 1,
                      cursor: msg.failed ? 'pointer' : 'default',
                    }}>
                    {msg.body}
                  </div>
                  <div style={{ fontSize: '10px', fontFamily: 'var(--mono)', color: 'var(--text-mute)', padding: '0 4px', display: 'flex', gap: '4px', alignItems: 'center' }}>
                    {msg.failed ? (
                      <span style={{ color: 'var(--red, #ef4444)', cursor: 'pointer' }} onClick={() => retryMessage(msg.id, msg.body)}>⚠ Tap to retry</span>
                    ) : (
                      <>
                        {formatTime(msg.createdAt)}
                        {msg.encrypted && <span title="Encrypted" style={{ opacity: 0.5 }}>🔒</span>}
                      </>
                    )}
                  </div>
                </div>
              </div>
            )
          })
        )}
        {activePeer && hasMore[activePeer.username] && (
          <div style={{ display: 'flex', justifyContent: 'center', padding: '8px 0 4px' }}>
            <button
              onClick={handleLoadMore}
              disabled={loadingMore}
              style={{
                background: 'none', border: '1px solid var(--border-2)',
                borderRadius: '999px', color: 'var(--text-dim)',
                fontSize: '12px', padding: '5px 16px', cursor: loadingMore ? 'default' : 'pointer',
                opacity: loadingMore ? 0.5 : 1,
              }}
            >
              {loadingMore ? 'Loading…' : 'Load more'}
            </button>
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Typing indicator */}
      {activePeer && typing[activePeer.username] && (
        <div style={{ padding: '4px 16px 8px', fontSize: '11px', color: 'var(--text-mute)', fontStyle: 'italic' }}>
          {activePeer.username} is typing…
        </div>
      )}

      {/* Input */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: '8px',
        padding: '0 12px', height: '64px',
        borderTop: '1px solid var(--border)', flexShrink: 0,
      }}>
        <textarea
          value={input}
          onChange={handleTyping}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend() } }}
          placeholder={peerHasKey ? 'Message (end-to-end encrypted)…' : 'Message…'}
          rows={1}
          style={{
            flex: 1, background: 'var(--bg-3)', border: '1px solid var(--border)',
            borderRadius: '999px', color: 'var(--text)', fontFamily: 'var(--font)',
            fontSize: '13.5px', padding: '9px 16px', outline: 'none', resize: 'none',
            lineHeight: 1.4,
          }}
        />
        <button
          onClick={handleSend}
          disabled={!input.trim()}
          style={{
            width: '38px', height: '38px', borderRadius: '50%',
            background: 'var(--blue)', border: 'none', color: 'white',
            cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
            flexShrink: 0, opacity: input.trim() ? 1 : 0.35,
          }}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <line x1="22" y1="2" x2="11" y2="13"/>
            <polygon points="22 2 15 22 11 13 2 9 22 2"/>
          </svg>
        </button>
      </div>
    </div>
  )
}