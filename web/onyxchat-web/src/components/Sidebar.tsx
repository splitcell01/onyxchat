import { useEffect, useState, useCallback } from 'react'
import { useChat } from '../context/ChatContext'
import { useAuth } from '../context/AuthContext'
import { SettingsPanel } from './components/SettingsPanel'

const initials = (name: string) => name.slice(0, 2).toUpperCase()

export function Sidebar() {
  const { user, logout } = useAuth()
  const { contacts, activePeer, unread, selectPeer, loadContacts } = useChat()
  const [showSettings, setShowSettings] = useState(false)
  const [showSettings, setShowSettings] = useState(false)

  useEffect(() => { loadContacts() }, [loadContacts])

  const online  = contacts.filter(c => c.online)
  const offline = contacts.filter(c => !c.online)

  return (
    <>
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

      {showSettings && <SettingsModal onClose={() => setShowSettings(false)} />}
    </>
  )
}

function SettingsModal({ onClose }: { onClose: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const [loading, setLoading] = useState(false)

  const handleSubmit = useCallback(async () => {
    setError('')
    setSuccess(false)
    if (!current || !next || !confirm) return setError('All fields required.')
    if (next.length < 8) return setError('New password must be at least 8 characters.')
    if (next !== confirm) return setError('New passwords do not match.')
    setLoading(true)
    try {
      await changePassword(current, next)
      setSuccess(true)
      setCurrent(''); setNext(''); setConfirm('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.')
    } finally {
      setLoading(false)
    }
  }, [current, next, confirm])

  // Close on backdrop click
  const handleBackdrop = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    <div
      onClick={handleBackdrop}
      style={{
        position: 'fixed', inset: 0, zIndex: 50,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: '20px',
      }}
    >
      <div style={{
        width: '100%', maxWidth: '360px',
        background: 'var(--bg-2)',
        border: '1px solid var(--border-2)',
        borderRadius: '16px',
        padding: '28px 28px 24px',
      }}>
        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '20px' }}>
          <div style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text)' }}>Change Password</div>
          <button onClick={onClose} style={{ background: 'transparent', border: 'none', color: 'var(--text-mute)', cursor: 'pointer', padding: '4px', borderRadius: '6px', display: 'flex' }}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>

        {/* Success */}
        {success && (
          <div style={{
            fontSize: '12px', color: 'var(--green)',
            background: 'rgba(34,197,94,0.08)',
            border: '1px solid rgba(34,197,94,0.2)',
            borderRadius: '8px', padding: '8px 12px', marginBottom: '12px',
          }}>
            Password changed successfully.
          </div>
        )}

        {/* Error */}
        {error && (
          <div style={{
            fontSize: '12px', color: 'var(--red)',
            background: 'rgba(239,68,68,0.08)',
            border: '1px solid rgba(239,68,68,0.2)',
            borderRadius: '8px', padding: '8px 12px', marginBottom: '12px',
          }}>
            {error}
          </div>
        )}

        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {[
            { label: 'Current Password', value: current, setter: setCurrent, auto: 'current-password' },
            { label: 'New Password', value: next, setter: setNext, auto: 'new-password' },
            { label: 'Confirm New Password', value: confirm, setter: setConfirm, auto: 'new-password' },
          ].map(({ label, value, setter, auto }) => (
            <div key={label}>
              <div style={{ fontSize: '12px', fontWeight: 500, color: 'var(--text-dim)', marginBottom: '4px' }}>{label}</div>
              <input
                type="password"
                value={value}
                onChange={e => setter(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleSubmit()}
                autoComplete={auto}
                placeholder="••••••••"
                style={{
                  width: '100%', background: 'var(--bg-3)',
                  border: '1px solid var(--border)', borderRadius: '8px',
                  color: 'var(--text)', fontSize: '14px', padding: '10px 14px',
                  outline: 'none', boxSizing: 'border-box',
                }}
              />
            </div>
          ))}

          <button
            onClick={handleSubmit}
            disabled={loading}
            style={{
              width: '100%', padding: '11px',
              background: 'var(--blue)', border: 'none',
              borderRadius: '8px', color: 'white',
              fontSize: '14px', fontWeight: 600, cursor: 'pointer',
              marginTop: '4px', opacity: loading ? 0.5 : 1,
            }}
          >
            {loading ? 'Updating…' : 'Update Password'}
          </button>
        </div>
      </div>
    </div>
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
