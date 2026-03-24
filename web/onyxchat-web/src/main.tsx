import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { AuthProvider } from './context/AuthContext.tsx'
import { ChatProvider } from './context/ChatContext.tsx'

const isAdmin = window.location.pathname === '/admin'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      {isAdmin ? <App /> : <ChatProvider><App /></ChatProvider>}
    </AuthProvider>
  </StrictMode>,
)