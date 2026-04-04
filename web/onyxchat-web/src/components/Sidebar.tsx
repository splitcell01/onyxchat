import { useEffect, useRef, useState } from 'react'
import { useChat } from '../context/ChatContext'
import { useAuth } from '../context/AuthContext'
import { SettingsPanel } from './SettingsPanel'
import { addContact } from '../api/contacts'
import { api } from '../api/client'

const initials = (name: string) => name.slice(0, 2).toUpperCase()

export function Sidebar() {
  const { user, logout } = useAuth()
  const { contacts, activePeer, unread, selectPeer, loadContacts } = useChat()
  const [showSettings, setShowSettings] = useState(false)
  const [showAddContact, setShowAddContact] = useState(false)
  const [addUsername, setAddUsername] = useState('')
  const [addError, setAddError] = useState('')
  const [addLoading, setAddLoading] = useState(false)
  const [addSuccess, setAddSuccess] = useState(false)
  const [searchResults, setSearchResults] = useState<{ id: number; username: string }[]>([])
  const [showDropdown, setShowDropdown] = useState(false)
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { loadContacts() }, [loadContacts])

  const online  = contacts.filter(c => c.online)
  const offline = contacts.filter(c => !c.online)

  function handleSearchInput(value: string) {
    setAddUsername(value)
    setAddError('')
    if (searchTimer.current) clearTimeout(searchTimer.current)
    if (value.trim().length < 1) {
      setSearchResults([])
      setShowDropdown(false)
      return
    }
    searchTimer.current = setTimeout(async () => {
      try {
        const results = await api.get<{ id: number; username: string }[]>(
          `/api/v1/users?search=${encodeURIComponent(value.trim())}`
        )
        setSearchResults(Array.isArray(results) ? results : [])
        setShowDropdown(true)
      } catch {
        setSearchResults([])
      }
    }, 200)
  }

  function handleSelectResult(username: string) {
    setAddUsername(username)
    setShowDropdown(false)
    setSearchResults([])
  }

  async function handleAddContact() {
    setAddError('')
    setAddSuccess(false)
    if (!addUsername.trim()) return setAddError('Enter a username.')
    setAddLoading(true)
    try {
      await addContact(addUsername.trim())
      await loadContacts()
      setAddSuccess(true)
      setAddUsername('')
      setSearchResults([])
      setTimeout(() => { setShowAddContact(false); setAddSuccess(false) }, 1200)
    } catch (e) {
      setAddError(e instanceof Error ? e.message : 'User not found.')
    } finally {
      setAddLoading(false)
    }
  }

  return (
    <>
      {/* Add Contact Modal */}
      {showAddContact && (
        <div style={{
          position: 'fixed', inset: 0,
          background: 'rgba(2,6,23,0.8)', backdropFilter: 'blur(4px)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          zIndex: 100, padding: '20px',
        }}>
          <div style={{
            width: '100%', maxWidth: '360px',
            background: 'var(--bg-2)', border: '1px solid var(--border-2)',
            borderRadius: '16px', padding: '24px',
            display: 'flex', flexDirection: 'column', gap: '16px',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text)' }}>Add Contact</span>
              <button onClick={() => { setShowAddContact(false); setAddUsername(''); setAddError(''); setAddSuccess(false); setSearchResults([]); setShowDropdown(false) }} style={{
                background: 'transparent', border: 'none', cursor: 'pointer',
                color: 'var(--text-mute)', fontSize: '18px', lineHeight: 1, padding: '2px 6px', borderRadius: '6px',
              }}>✕</button>
            </div>
            <div style={{ position: 'relative' }}>
              <input
                autoFocus
                value={addUsername}
                onChange={e => handleSearchInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { setShowDropdown(false); handleAddContact() } if (e.key === 'Escape') setShowDropdown(false) }}
                onBlur={() => setTimeout(() => setShowDropdown(false), 150)}
                placeholder="Search by username…"
                style={{
                  width: '100%', background: 'var(--bg-3)', border: '1px solid var(--border)',
                  borderRadius: '8px', color: 'var(--text)', fontSize: '14px',
                  padding: '10px 14px', outline: 'none', boxSizing: 'border-box',
                }}
              />
              {showDropdown && searchResults.length > 0 && (
                <div style={{
                  position: 'absolute', top: 'calc(100% + 4px)', left: 0, right: 0,
                  background: 'var(--bg-3)', border: '1px solid var(--border-2)',
                  borderRadius: '8px', overflow: 'hidden', zIndex: 10,
                  boxShadow: '0 4px 16px rgba(0,0,0,0.4)',
                }}>
                  {searchResults.map(u => (
                    <div
                      key={u.id}
                      onMouseDown={() => handleSelectResult(u.username)}
                      style={{
                        padding: '9px 14px', cursor: 'pointer', fontSize: '13px',
                        color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '8px',
                      }}
                      onMouseEnter={e => (e.currentTarget.style.background = 'var(--surface)')}
                      onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                    >
                      <span style={{
                        width: '28px', height: '28px', borderRadius: '50%',
                        background: 'var(--surface)', border: '1px solid var(--border-2)',
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                        fontSize: '11px', fontWeight: 600, color: 'var(--text-dim)', flexShrink: 0,
                      }}>
                        {u.username.slice(0, 2).toUpperCase()}
                      </span>
                      {u.username}
                    </div>
                  ))}
                </div>
              )}
              {showDropdown && searchResults.length === 0 && addUsername.trim().length > 0 && (
                <div style={{
                  position: 'absolute', top: 'calc(100% + 4px)', left: 0, right: 0,
                  background: 'var(--bg-3)', border: '1px solid var(--border-2)',
                  borderRadius: '8px', padding: '10px 14px', zIndex: 10,
                  fontSize: '12px', color: 'var(--text-mute)',
                }}>
                  No users found
                </div>
              )}
            </div>
            {addError && (
              <div style={{
                fontSize: '12px', color: 'var(--red)',
                background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)',
                borderRadius: '8px', padding: '8px 12px',
              }}>{addError}</div>
            )}
            {addSuccess && (
              <div style={{
                fontSize: '12px', color: 'var(--green)',
                background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.2)',
                borderRadius: '8px', padding: '8px 12px',
              }}>Contact added! ✓</div>
            )}
            <button onClick={handleAddContact} disabled={addLoading} style={{
              padding: '10px', background: 'var(--blue)', border: 'none',
              borderRadius: '8px', color: 'white', fontSize: '13px',
              fontWeight: 600, cursor: addLoading ? 'not-allowed' : 'pointer',
              opacity: addLoading ? 0.6 : 1,
            }}>
              {addLoading ? 'Adding…' : 'Add Contact'}
            </button>
          </div>
        </div>
      )}

      <div style={{
        width: '260px', minWidth: '260px',
        display: 'flex', flexDirection: 'column',
        background: 'var(--bg-2)',
        borderRight: '1px solid var(--border)',
      }}>
        {/* Header */}
        <div style={{
          height: '56px', display: 'flex', alignItems: 'center',
          padding: '0 16px', borderBottom: '1px solid var(--border)', gap: '10px',
        }}>
          <svg width="28" height="28" viewBox="0 0 28 28" fill="none">
            <circle cx="14" cy="14" r="8" stroke="#2563eb" strokeWidth="1.8"/>
            <circle cx="14" cy="14" r="3.5" fill="#2563eb"/>
            <circle cx="14" cy="6" r="1.5" fill="#2563eb" opacity="0.4"/>
          </svg>
          <span style={{ fontWeight: 600, fontSize: '15px', color: 'var(--text)' }}>OnyxChat</span>
        </div>

        {/* Add Contact Button */}
        <div style={{ padding: '8px 10px 0' }}>
          <button onClick={() => { setShowAddContact(true); setAddError(''); setAddUsername('') }} style={{
            width: '100%', display: 'flex', alignItems: 'center', gap: '8px',
            padding: '8px 12px', borderRadius: '8px',
            background: 'var(--blue-glow)', border: '1px dashed rgba(37,99,235,0.4)',
            color: 'var(--blue)', fontSize: '13px', fontWeight: 500,
            cursor: 'pointer',
          }}>
            <span style={{ fontSize: '16px', lineHeight: 1 }}>+</span>
            Add Contact
          </button>
        </div>

        {/* Contacts */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {online.length > 0 && (
            <>
              <div style={{ fontSize: '10px', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-mute)', padding: '10px 16px 4px' }}>
                Online — {online.length}
              </div>
              {online.map(c => <ContactItem key={c.id} contact={c} active={activePeer?.username === c.username} unread={unread[c.username] ?? 0} onClick={() => selectPeer(c.username)} />)}
            </>
          )}
          {offline.length > 0 && (
            <>
              <div style={{ fontSize: '10px', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-mute)', padding: '10px 16px 4px' }}>
                Contacts
              </div>
              {offline.map(c => <ContactItem key={c.id} contact={c} active={activePeer?.username === c.username} unread={unread[c.username] ?? 0} onClick={() => selectPeer(c.username)} />)}
            </>
          )}
          {contacts.length === 0 && (
            <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-mute)', fontSize: '12px' }}>
              No contacts yet
            </div>
          )}
        </div>

        {/* Footer */}
        <div style={{
          padding: '12px', borderTop: '1px solid var(--border)',
          display: 'flex', alignItems: 'center', gap: '10px',
        }}>
          <div style={{
            width: '36px', height: '36px', borderRadius: '50%',
            background: 'var(--surface)', border: '1px solid var(--border-2)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: '14px', fontWeight: 600, color: 'var(--text-dim)', flexShrink: 0,
          }}>
            {user ? initials(user.username) : '?'}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {user?.username}
            </div>
            <div style={{ fontSize: '11px', color: 'var(--green)', display: 'flex', alignItems: 'center', gap: '4px' }}>
              <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: 'var(--green)', display: 'inline-block' }} />
              online
            </div>
          </div>
          {/* Settings button */}
          <button onClick={() => setShowSettings(true)} title="Settings" style={{
            background: 'transparent', border: 'none', color: 'var(--text-mute)',
            cursor: 'pointer', padding: '6px', borderRadius: '8px', display: 'flex',
          }}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="3"/>
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
            </svg>
          </button>
          <button onClick={logout} title="Sign out" style={{
            background: 'transparent', border: 'none', color: 'var(--text-mute)',
            cursor: 'pointer', padding: '6px', borderRadius: '8px', display: 'flex',
          }}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
              <polyline points="16 17 21 12 16 7"/>
              <line x1="21" y1="12" x2="9" y2="12"/>
            </svg>
          </button>
        </div>
      </div>

      {showSettings && <SettingsPanel onClose={() => setShowSettings(false)} />}
    </>
  )
}

function ContactItem({ contact, active, unread, onClick }: {
  contact: { username: string; online: boolean }
  active: boolean
  unread: number
  onClick: () => void
}) {
  return (
    <div onClick={onClick} style={{
      display: 'flex', alignItems: 'center', gap: '10px',
      padding: '9px 12px', cursor: 'pointer', borderRadius: '8px',
      margin: '1px 6px', background: active ? 'var(--surface)' : 'transparent',
    }}>
      <div style={{
        width: '36px', height: '36px', borderRadius: '50%',
        background: 'var(--surface)', border: '1px solid var(--border-2)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        fontSize: '14px', fontWeight: 600, color: 'var(--text-dim)',
        flexShrink: 0, position: 'relative',
      }}>
        {initials(contact.username)}
        <div style={{
          position: 'absolute', bottom: '1px', right: '1px',
          width: '9px', height: '9px', borderRadius: '50%',
          background: contact.online ? 'var(--green)' : 'var(--text-mute)',
          border: '2px solid var(--bg-2)',
        }} />
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: '13px', fontWeight: 500, color: active ? 'var(--text)' : 'var(--text-dim)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {contact.username}
        </div>
      </div>
      {unread > 0 && (
        <div style={{
          background: 'var(--blue)', color: 'white', fontSize: '10px',
          fontWeight: 600, padding: '2px 6px', borderRadius: '999px', minWidth: '18px', textAlign: 'center',
        }}>
          {unread}
        </div>
      )}
    </div>
  )
}
