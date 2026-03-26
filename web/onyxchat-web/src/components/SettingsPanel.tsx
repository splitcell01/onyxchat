import { useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { changePassword } from '../api/auth'
import { deleteAccount } from '../api/contacts'

type Section = 'account' | 'danger'

interface SettingsPanelProps {
  onClose: () => void
}

export function SettingsPanel({ onClose }: SettingsPanelProps) {
  const { user, logout } = useAuth()
  const [section, setSection] = useState<Section>('account')

  // Change password state
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [pwError, setPwError] = useState('')
  const [pwSuccess, setPwSuccess] = useState(false)
  const [pwLoading, setPwLoading] = useState(false)

  // Delete account state
  const [deletePassword, setDeletePassword] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState('')
  const [deleteError, setDeleteError] = useState('')
  const [deleteLoading, setDeleteLoading] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

  async function handleChangePassword() {
    setPwError('')
    setPwSuccess(false)
    if (!currentPassword || !newPassword || !confirmPassword)
      return setPwError('All fields are required.')
    if (newPassword.length < 8)
      return setPwError('New password must be at least 8 characters.')
    if (newPassword !== confirmPassword)
      return setPwError('New passwords do not match.')

    setPwLoading(true)
    try {
      await changePassword(currentPassword, newPassword)
      setPwSuccess(true)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (e) {
      setPwError(e instanceof Error ? e.message : 'Failed to change password.')
    } finally {
      setPwLoading(false)
    }
  }

  async function handleDeleteAccount() {
    setDeleteError('')
    if (!deletePassword) return setDeleteError('Password is required.')
    if (deleteConfirm !== 'delete my account')
      return setDeleteError('Please type "delete my account" exactly to confirm.')

    setDeleteLoading(true)
    try {
      await deleteAccount(deletePassword)
      await logout()
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : 'Failed to delete account.')
      setDeleteLoading(false)
    }
  }

  const inputStyle: React.CSSProperties = {
    width: '100%',
    background: 'var(--bg-3)',
    border: '1px solid var(--border)',
    borderRadius: '8px',
    color: 'var(--text)',
    fontSize: '14px',
    padding: '10px 14px',
    outline: 'none',
    boxSizing: 'border-box',
  }

  const labelStyle: React.CSSProperties = {
    fontSize: '12px',
    fontWeight: 500,
    color: 'var(--text-dim)',
    marginBottom: '4px',
  }

  return (
    <div style={{
      position: 'fixed', inset: 0,
      background: 'rgba(2,6,23,0.8)',
      backdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      padding: '20px',
      zIndex: 100,
    }}>
      <div style={{
        width: '100%', maxWidth: '480px',
        background: 'var(--bg-2)',
        border: '1px solid var(--border-2)',
        borderRadius: '20px',
        overflow: 'hidden',
        display: 'flex', flexDirection: 'column',
        maxHeight: '90vh',
      }}>

        {/* Header */}
        <div style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          padding: '20px 24px',
          borderBottom: '1px solid var(--border)',
        }}>
          <span style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text)' }}>
            Settings
          </span>
          <button onClick={onClose} style={{
            background: 'transparent', border: 'none', cursor: 'pointer',
            color: 'var(--text-mute)', fontSize: '18px', lineHeight: 1,
            padding: '2px 6px', borderRadius: '6px',
          }}>✕</button>
        </div>

        <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>

          {/* Sidebar nav */}
          <div style={{
            width: '140px', flexShrink: 0,
            borderRight: '1px solid var(--border)',
            padding: '12px 8px',
            display: 'flex', flexDirection: 'column', gap: '2px',
          }}>
            {([
              { id: 'account', label: 'Account' },
              { id: 'danger',  label: 'Danger Zone' },
            ] as { id: Section; label: string }[]).map(({ id, label }) => (
              <button key={id} onClick={() => setSection(id)} style={{
                width: '100%', textAlign: 'left',
                padding: '8px 12px', borderRadius: '8px',
                border: 'none', cursor: 'pointer', fontSize: '13px', fontWeight: 500,
                background: section === id ? 'var(--surface)' : 'transparent',
                color: id === 'danger'
                  ? section === id ? 'var(--red)' : 'rgba(239,68,68,0.7)'
                  : section === id ? 'var(--text)' : 'var(--text-mute)',
                transition: 'background 0.15s, color 0.15s',
              }}>
                {label}
              </button>
            ))}
          </div>

          {/* Content */}
          <div style={{ flex: 1, overflowY: 'auto', padding: '24px' }}>

            {/* ── Account section ── */}
            {section === 'account' && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>

                {/* User info */}
                <div style={{
                  display: 'flex', alignItems: 'center', gap: '12px',
                  padding: '14px 16px',
                  background: 'var(--bg-3)', borderRadius: '12px',
                  border: '1px solid var(--border)',
                }}>
                  <div style={{
                    width: '36px', height: '36px', borderRadius: '50%',
                    background: 'var(--blue)', display: 'flex',
                    alignItems: 'center', justifyContent: 'center',
                    fontSize: '15px', fontWeight: 700, color: 'white', flexShrink: 0,
                  }}>
                    {user?.username?.[0]?.toUpperCase() ?? '?'}
                  </div>
                  <div>
                    <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>
                      {user?.username}
                    </div>
                    <div style={{ fontSize: '12px', color: 'var(--text-mute)' }}>
                      ID: {user?.id}
                    </div>
                  </div>
                </div>

                {/* Change password */}
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text)', marginBottom: '14px' }}>
                    Change Password
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                    <div>
                      <div style={labelStyle}>Current Password</div>
                      <input
                        type="password" value={currentPassword}
                        onChange={e => setCurrentPassword(e.target.value)}
                        placeholder="••••••••"
                        autoComplete="current-password"
                        style={inputStyle}
                      />
                    </div>
                    <div>
                      <div style={labelStyle}>New Password</div>
                      <input
                        type="password" value={newPassword}
                        onChange={e => setNewPassword(e.target.value)}
                        placeholder="min 8 characters"
                        autoComplete="new-password"
                        style={inputStyle}
                      />
                    </div>
                    <div>
                      <div style={labelStyle}>Confirm New Password</div>
                      <input
                        type="password" value={confirmPassword}
                        onChange={e => setConfirmPassword(e.target.value)}
                        placeholder="••••••••"
                        autoComplete="new-password"
                        style={inputStyle}
                      />
                    </div>

                    {pwError && (
                      <div style={{
                        fontSize: '12px', color: 'var(--red)',
                        background: 'rgba(239,68,68,0.08)',
                        border: '1px solid rgba(239,68,68,0.2)',
                        borderRadius: '8px', padding: '8px 12px',
                      }}>{pwError}</div>
                    )}
                    {pwSuccess && (
                      <div style={{
                        fontSize: '12px', color: 'var(--green)',
                        background: 'rgba(34,197,94,0.08)',
                        border: '1px solid rgba(34,197,94,0.2)',
                        borderRadius: '8px', padding: '8px 12px',
                      }}>Password updated successfully.</div>
                    )}

                    <button onClick={handleChangePassword} disabled={pwLoading} style={{
                      padding: '10px', background: 'var(--blue)', border: 'none',
                      borderRadius: '8px', color: 'white', fontSize: '13px',
                      fontWeight: 600, cursor: 'pointer', opacity: pwLoading ? 0.5 : 1,
                    }}>
                      {pwLoading ? 'Updating…' : 'Update Password'}
                    </button>
                  </div>
                </div>
              </div>
            )}

            {/* ── Danger Zone section ── */}
            {section === 'danger' && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div style={{
                  padding: '14px 16px',
                  background: 'rgba(239,68,68,0.06)',
                  border: '1px solid rgba(239,68,68,0.2)',
                  borderRadius: '12px',
                  fontSize: '13px', color: 'var(--text-dim)', lineHeight: 1.6,
                }}>
                  ⚠️ Deleting your account is <strong style={{ color: 'var(--text)' }}>permanent and irreversible</strong>.
                  All your messages, contacts, and encryption keys will be erased immediately.
                </div>

                {!showDeleteConfirm ? (
                  <button onClick={() => setShowDeleteConfirm(true)} style={{
                    padding: '10px 16px',
                    background: 'transparent',
                    border: '1px solid rgba(239,68,68,0.4)',
                    borderRadius: '8px', color: 'var(--red)',
                    fontSize: '13px', fontWeight: 600, cursor: 'pointer',
                    transition: 'background 0.15s',
                  }}>
                    Delete My Account
                  </button>
                ) : (
                  <div style={{
                    display: 'flex', flexDirection: 'column', gap: '10px',
                    padding: '16px',
                    background: 'rgba(239,68,68,0.06)',
                    border: '1px solid rgba(239,68,68,0.25)',
                    borderRadius: '12px',
                  }}>
                    <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--red)' }}>
                      Confirm account deletion
                    </div>

                    <div>
                      <div style={labelStyle}>Your Password</div>
                      <input
                        type="password" value={deletePassword}
                        onChange={e => setDeletePassword(e.target.value)}
                        placeholder="Enter your password"
                        autoComplete="current-password"
                        style={inputStyle}
                      />
                    </div>

                    <div>
                      <div style={labelStyle}>
                        Type <span style={{ color: 'var(--text)', fontFamily: 'var(--mono)', fontSize: '11px' }}>delete my account</span> to confirm
                      </div>
                      <input
                        value={deleteConfirm}
                        onChange={e => setDeleteConfirm(e.target.value)}
                        placeholder="delete my account"
                        style={inputStyle}
                      />
                    </div>

                    {deleteError && (
                      <div style={{
                        fontSize: '12px', color: 'var(--red)',
                        background: 'rgba(239,68,68,0.08)',
                        border: '1px solid rgba(239,68,68,0.2)',
                        borderRadius: '8px', padding: '8px 12px',
                      }}>{deleteError}</div>
                    )}

                    <div style={{ display: 'flex', gap: '8px', marginTop: '4px' }}>
                      <button onClick={() => {
                        setShowDeleteConfirm(false)
                        setDeletePassword('')
                        setDeleteConfirm('')
                        setDeleteError('')
                      }} style={{
                        flex: 1, padding: '10px',
                        background: 'var(--bg-3)', border: '1px solid var(--border)',
                        borderRadius: '8px', color: 'var(--text-dim)',
                        fontSize: '13px', fontWeight: 500, cursor: 'pointer',
                      }}>
                        Cancel
                      </button>
                      <button
                        onClick={handleDeleteAccount}
                        disabled={deleteLoading || deleteConfirm !== 'delete my account'}
                        style={{
                          flex: 1, padding: '10px',
                          background: 'var(--red)', border: 'none',
                          borderRadius: '8px', color: 'white',
                          fontSize: '13px', fontWeight: 600,
                          cursor: deleteLoading || deleteConfirm !== 'delete my account' ? 'not-allowed' : 'pointer',
                          opacity: deleteLoading || deleteConfirm !== 'delete my account' ? 0.5 : 1,
                          transition: 'opacity 0.15s',
                        }}
                      >
                        {deleteLoading ? 'Deleting…' : 'Delete Account'}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}