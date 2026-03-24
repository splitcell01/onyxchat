// src/components/AdminPanel.tsx
import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../context/AuthContext'
import { adminApi, type InviteCode } from '../api/admin'
import { api } from '../api/client'
import type { User } from '../types'

// ── Tiny shared primitives ────────────────────────────────────

function Badge({ status }: { status: 'available' | 'used' | 'expired' }) {
  const styles: Record<string, React.CSSProperties> = {
    available: { background: 'rgba(34,197,94,0.12)', color: 'var(--green)', border: '1px solid rgba(34,197,94,0.2)' },
    used:      { background: 'var(--bg-3)',           color: 'var(--text-mute)', border: '1px solid var(--border-2)' },
    expired:   { background: 'rgba(239,68,68,0.10)',  color: 'var(--red)',  border: '1px solid rgba(239,68,68,0.2)' },
  }
  const labels = { available: 'Available', used: 'Used', expired: 'Expired' }
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      padding: '3px 9px', borderRadius: 20,
      fontSize: 11, fontWeight: 600, letterSpacing: '0.04em',
      ...styles[status],
    }}>
      <span style={{ width: 5, height: 5, borderRadius: '50%', background: 'currentColor' }} />
      {labels[status]}
    </span>
  )
}

function StatCard({ label, value, color }: { label: string; value: string | number; color: string }) {
  return (
    <div style={{
      background: 'var(--bg-2)', border: '1px solid var(--border)',
      borderRadius: 12, padding: '16px 20px',
    }}>
      <div style={{ fontSize: 11, fontWeight: 500, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-mute)', marginBottom: 8 }}>
        {label}
      </div>
      <div style={{ fontSize: 28, fontWeight: 700, letterSpacing: '-0.03em', color }}>
        {value}
      </div>
    </div>
  )
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const copy = () => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  return (
    <button onClick={copy} title="Copy" style={{
      background: 'none', border: 'none', cursor: 'pointer',
      color: copied ? 'var(--green)' : 'var(--text-mute)',
      padding: '2px 4px', borderRadius: 4, fontSize: 13,
      transition: 'color 0.15s', marginLeft: 4,
    }}>
      {copied ? '✓' : '⎘'}
    </button>
  )
}

// ── Helpers ───────────────────────────────────────────────────

function fmtDate(iso: string) {
  const d = new Date(iso)
  const now = new Date()
  const diff = d.getTime() - now.getTime()
  const abs = Math.abs(diff)
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (abs < 1000 * 60 * 60 * 24 * 3) {
    if (abs < 1000 * 60 * 60) return rtf.format(Math.round(diff / 60000), 'minute')
    if (abs < 1000 * 60 * 60 * 24) return rtf.format(Math.round(diff / 3600000), 'hour')
    return rtf.format(Math.round(diff / 86400000), 'day')
  }
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

function codeStatus(c: InviteCode): 'available' | 'used' | 'expired' {
  if (c.used_by) return 'used'
  if (c.expires_at && new Date(c.expires_at) < new Date()) return 'expired'
  return 'available'
}

const CHARSET = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789'
function randomCode(prefix: string) {
  const b = Array.from({ length: 6 }, () => CHARSET[Math.floor(Math.random() * CHARSET.length)])
  return `${prefix}-${b.slice(0, 3).join('')}-${b.slice(3).join('')}`
}

// ── Main component ────────────────────────────────────────────

export function AdminPanel() {
  const { user, logout } = useAuth()
  const [tab, setTab] = useState<'invites' | 'users'>('invites')
  const [codes, setCodes] = useState<InviteCode[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [newCode, setNewCode] = useState('')
  const [prefix, setPrefix] = useState('ONYX-ALPHA')
  const [expiry, setExpiry] = useState(30)
  const [generating, setGenerating] = useState(false)

  const loadData = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [c, u] = await Promise.all([
        adminApi.listInvites(),
        api.get<User[]>('/api/v1/users'),
      ])
      setCodes(Array.isArray(c) ? c : [])
      setUsers(Array.isArray(u) ? u : [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load data')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  async function generate() {
    setGenerating(true)
    setError('')
    const code = randomCode(prefix)
    try {
      await adminApi.createInvite({ code, expires_days: expiry || undefined })
      setNewCode(code)
      showSuccess(`${code} created!`)
      loadData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create invite')
    } finally {
      setGenerating(false)
    }
  }

  async function reset(code: string) {
    if (!confirm(`Reset ${code}? It can be used again.`)) return
    try {
      await adminApi.resetInvite(code)
      showSuccess(`${code} reset.`)
      loadData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to reset invite')
    }
  }

  function showSuccess(msg: string) {
    setSuccess(msg)
    setTimeout(() => setSuccess(''), 3500)
  }

  const available = codes.filter(c => codeStatus(c) === 'available').length
  const used      = codes.filter(c => codeStatus(c) === 'used').length

  // ── Guard ───────────────────────────────────────────────────
  if (user?.username !== 'ashenspellbook') {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', background: 'var(--bg)' }}>
        <div style={{ textAlign: 'center', color: 'var(--text-mute)' }}>
          <div style={{ fontSize: 48, marginBottom: 12 }}>⛔</div>
          <div style={{ fontSize: 14, fontWeight: 500 }}>Admin access only</div>
        </div>
      </div>
    )
  }

  // ── Shared styles ───────────────────────────────────────────
  const card: React.CSSProperties = {
    background: 'var(--bg-2)', border: '1px solid var(--border)',
    borderRadius: 14, padding: '20px 24px', marginBottom: 20,
  }

  const btn = (variant: 'primary' | 'ghost' | 'sm-ghost'): React.CSSProperties => ({
    padding: variant === 'sm-ghost' ? '5px 12px' : '8px 18px',
    borderRadius: 8, border: variant === 'ghost' || variant === 'sm-ghost' ? '1px solid var(--border-2)' : 'none',
    background: variant === 'primary' ? 'var(--blue)' : 'var(--bg-3)',
    color: variant === 'primary' ? 'white' : 'var(--text-dim)',
    fontSize: variant === 'sm-ghost' ? 12 : 13, fontWeight: 600,
    cursor: 'pointer', fontFamily: 'var(--font)', whiteSpace: 'nowrap',
  })

  const input: React.CSSProperties = {
    background: 'var(--bg-3)', border: '1px solid var(--border-2)',
    borderRadius: 8, color: 'var(--text)', fontFamily: 'var(--font)',
    fontSize: 13, padding: '8px 12px', outline: 'none',
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: 'var(--bg)', overflow: 'auto' }}>

      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '0 32px', height: 56, borderBottom: '1px solid var(--border)',
        background: 'var(--bg-2)', position: 'sticky', top: 0, zIndex: 10, flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, fontWeight: 700, fontSize: 15 }}>
          <svg width="26" height="26" viewBox="0 0 36 36" fill="none">
            <rect width="36" height="36" rx="8" fill="#050d1e"/>
            <circle cx="18" cy="18" r="9" stroke="#2563eb" strokeWidth="2"/>
            <circle cx="18" cy="18" r="4" fill="#2563eb"/>
            <circle cx="18" cy="9" r="2" fill="#2563eb" opacity="0.4"/>
          </svg>
          OnyxChat
          <span style={{
            fontSize: 10, fontWeight: 600, letterSpacing: '0.08em',
            background: 'var(--blue)', color: 'white',
            padding: '2px 7px', borderRadius: 4, textTransform: 'uppercase',
          }}>Admin</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8,
            background: 'var(--bg-3)', border: '1px solid var(--border-2)',
            borderRadius: 20, padding: '5px 12px 5px 8px',
            fontSize: 13, color: 'var(--text-dim)',
          }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--green)', boxShadow: '0 0 6px var(--green)', display: 'inline-block' }} />
            {user.username}
          </div>
          <button onClick={logout} style={{
            background: 'none', border: '1px solid var(--border-2)',
            color: 'var(--text-mute)', borderRadius: 7,
            padding: '5px 12px', fontFamily: 'var(--font)',
            fontSize: 12, cursor: 'pointer',
          }}>
            Sign out
          </button>
        </div>
      </div>

      {/* Main */}
      <div style={{ flex: 1, maxWidth: 960, width: '100%', margin: '0 auto', padding: '28px 24px' }}>

        {/* Alerts */}
        {error && (
          <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 8, padding: '10px 14px', marginBottom: 16, fontSize: 12, color: 'var(--red)', display: 'flex', gap: 8 }}>
            <span>✕</span><span>{error}</span>
          </div>
        )}
        {success && (
          <div style={{ background: 'rgba(34,197,94,0.12)', border: '1px solid rgba(34,197,94,0.2)', borderRadius: 8, padding: '10px 14px', marginBottom: 16, fontSize: 12, color: 'var(--green)', display: 'flex', gap: 8 }}>
            <span>✓</span><span>{success}</span>
          </div>
        )}

        {/* Stats */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3,1fr)', gap: 12, marginBottom: 24 }}>
          <StatCard label="Registered Users" value={users.length || '—'} color="var(--blue)" />
          <StatCard label="Available Codes"  value={codes.length ? available : '—'} color="var(--green)" />
          <StatCard label="Used Codes"       value={codes.length ? used : '—'} color="var(--amber,#f59e0b)" />
        </div>

        {/* Tabs */}
        <div style={{
          display: 'flex', gap: 4, background: 'var(--bg-3)',
          border: '1px solid var(--border)', borderRadius: 10,
          padding: 4, marginBottom: 24, width: 'fit-content',
        }}>
          {(['invites', 'users'] as const).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              padding: '7px 18px', borderRadius: 7,
              border: tab === t ? '1px solid var(--border-2)' : 'none',
              background: tab === t ? 'var(--bg-2)' : 'transparent',
              color: tab === t ? 'var(--text)' : 'var(--text-mute)',
              fontSize: 13, fontWeight: 500, cursor: 'pointer', fontFamily: 'var(--font)',
            }}>
              {t === 'invites' ? 'Invite Codes' : 'Users'}
            </button>
          ))}
        </div>

        {/* Invite Codes tab */}
        {tab === 'invites' && (
          <>
            {/* Generate card */}
            <div style={card}>
              <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-mute)', marginBottom: 16 }}>
                Generate New Invite Code
              </div>
              <div style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  <label style={{ fontSize: 11, fontWeight: 500, color: 'var(--text-dim)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Prefix</label>
                  <input value={prefix} onChange={e => setPrefix(e.target.value)} style={{ ...input, width: 140 }} />
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  <label style={{ fontSize: 11, fontWeight: 500, color: 'var(--text-dim)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Expires In</label>
                  <select value={expiry} onChange={e => setExpiry(Number(e.target.value))} style={{ ...input, cursor: 'pointer' }}>
                    <option value={7}>7 days</option>
                    <option value={14}>14 days</option>
                    <option value={30}>30 days</option>
                    <option value={90}>90 days</option>
                    <option value={0}>Never</option>
                  </select>
                </div>
                <button onClick={generate} disabled={generating} style={{ ...btn('primary'), opacity: generating ? 0.5 : 1 }}>
                  {generating ? 'Generating…' : 'Generate'}
                </button>
                <button onClick={loadData} disabled={loading} style={btn('ghost')}>
                  ↻ Refresh
                </button>
              </div>

              {newCode && (
                <div style={{
                  marginTop: 14, background: 'var(--bg-3)', border: '1px solid var(--border-2)',
                  borderRadius: 10, padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 12,
                }}>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-mute)', marginBottom: 4, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.04em' }}>New Code Ready</div>
                    <span style={{ fontFamily: 'var(--mono)', fontSize: 15, fontWeight: 500, color: 'var(--green)', letterSpacing: '0.06em' }}>{newCode}</span>
                  </div>
                  <CopyButton text={newCode} />
                </div>
              )}
            </div>

            {/* Codes table */}
            <div style={card}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-mute)' }}>
                  All Invite Codes
                </div>
                {loading && <span style={{ fontSize: 12, color: 'var(--text-mute)' }}>Loading…</span>}
              </div>
              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                  <thead>
                    <tr>
                      {['Code', 'Status', 'Used By', 'Used At', 'Expires', ''].map(h => (
                        <th key={h} style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-mute)', padding: '0 12px 10px', textAlign: 'left', borderBottom: '1px solid var(--border)' }}>
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {codes.length === 0 && !loading && (
                      <tr><td colSpan={6} style={{ textAlign: 'center', padding: 40, color: 'var(--text-mute)', fontSize: 13 }}>No invite codes found.</td></tr>
                    )}
                    {codes.map(c => (
                      <tr key={c.id} style={{ borderBottom: '1px solid var(--border)' }}>
                        <td style={{ padding: '11px 12px', fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text)', letterSpacing: '0.04em' }}>
                          {c.code}<CopyButton text={c.code} />
                        </td>
                        <td style={{ padding: '11px 12px' }}><Badge status={codeStatus(c)} /></td>
                        <td style={{ padding: '11px 12px', fontSize: 13, color: 'var(--text-dim)' }}>{c.used_by ?? <span style={{ color: 'var(--text-mute)' }}>—</span>}</td>
                        <td style={{ padding: '11px 12px', fontSize: 13, color: 'var(--text-dim)' }}>{c.used_at ? fmtDate(c.used_at) : <span style={{ color: 'var(--text-mute)' }}>—</span>}</td>
                        <td style={{ padding: '11px 12px', fontSize: 13, color: 'var(--text-dim)' }}>{c.expires_at ? fmtDate(c.expires_at) : <span style={{ color: 'var(--text-mute)' }}>Never</span>}</td>
                        <td style={{ padding: '11px 12px', textAlign: 'right' }}>
                          {codeStatus(c) === 'used' && (
                            <button onClick={() => reset(c.code)} style={btn('sm-ghost')}>Reset</button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </>
        )}

        {/* Users tab */}
        {tab === 'users' && (
          <div style={card}>
            <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-mute)', marginBottom: 16 }}>
              Registered Users
            </div>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  {['Username', 'ID'].map(h => (
                    <th key={h} style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-mute)', padding: '0 12px 10px', textAlign: 'left', borderBottom: '1px solid var(--border)' }}>
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {users.length === 0 && (
                  <tr><td colSpan={2} style={{ textAlign: 'center', padding: 40, color: 'var(--text-mute)', fontSize: 13 }}>No users found.</td></tr>
                )}
                {users.map(u => (
                  <tr key={u.id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ padding: '11px 12px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        <div style={{
                          width: 28, height: 28, borderRadius: '50%',
                          background: 'rgba(37,99,235,0.18)', border: '1px solid var(--blue)',
                          display: 'flex', alignItems: 'center', justifyContent: 'center',
                          fontSize: 11, fontWeight: 700, color: 'var(--blue)', flexShrink: 0,
                        }}>
                          {u.username[0].toUpperCase()}
                        </div>
                        <span style={{ fontSize: 13, fontWeight: 500, color: 'var(--text)' }}>{u.username}</span>
                      </div>
                    </td>
                    <td style={{ padding: '11px 12px', fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--text-dim)' }}>{u.id}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}