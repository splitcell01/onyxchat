// src/App.tsx
import { useAuth } from './context/AuthContext'
import { AuthScreen } from './components/AuthScreen'
import { Sidebar } from './components/Sidebar'
import { ChatPanel } from './components/ChatPanel'
import { AdminPanel } from './components/AdminPanel'

export default function App() {
  const { isAuthenticated, user } = useAuth()

  if (!isAuthenticated) return <AuthScreen />

  // Admin route — /admin
  if (window.location.pathname === '/admin') {
    return <AdminPanel />
  }

  return (
    <div style={{ display: 'flex', height: '100vh', width: '100%', overflow: 'hidden' }}>
      <Sidebar />
      <ChatPanel />
    </div>
  )
}
