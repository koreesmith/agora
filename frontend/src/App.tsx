import { useEffect, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuthStore } from './store/auth'
import api from './api'
import Layout from './components/layout/Layout'

import SetupPage          from './pages/SetupPage'
import LandingPage        from './pages/LandingPage'
import LoginPage          from './pages/LoginPage'
import RegisterPage       from './pages/RegisterPage'
import VerifyEmailPage    from './pages/VerifyEmailPage'
import ResetPasswordPage  from './pages/ResetPasswordPage'
import ChangePasswordPage from './pages/ChangePasswordPage'
import PrivacyPage        from './pages/PrivacyPage'
import TermsPage          from './pages/TermsPage'
import FeedPage           from './pages/FeedPage'
import ProfilePage        from './pages/ProfilePage'
import FriendsPage        from './pages/FriendsPage'
import SearchPage         from './pages/SearchPage'
import NotificationsPage  from './pages/NotificationsPage'
import SettingsPage       from './pages/SettingsPage'
import AdminPage          from './pages/AdminPage'
import PostPage           from './pages/PostPage'
import DiscoverPage       from './pages/DiscoverPage'
import ListFeedPage        from './pages/ListFeedPage'
import GroupsPage          from './pages/GroupsPage'
import GroupPage           from './pages/GroupPage'
import InvitePage          from './pages/InvitePage'
import AlbumsPage          from './pages/AlbumsPage'
import AlbumPage           from './pages/AlbumPage'
import MessagesPage        from './pages/MessagesPage'

const qc = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
})

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuthStore()
  if (!isAuthenticated) return <Navigate to="/login" replace />
  return <>{children}</>
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { user } = useAuthStore()
  if (user?.role !== 'admin' && user?.role !== 'moderator') return <Navigate to="/feed" replace />
  return <>{children}</>
}

function GuestOnly({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuthStore()
  return isAuthenticated ? <Navigate to="/feed" replace /> : <>{children}</>
}

function AppRoutes() {
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)

  useEffect(() => {
    api.get('/setup').then(r => setNeedsSetup(r.data.needs_setup)).catch(() => setNeedsSetup(false))
  }, [])

  if (needsSetup === null) return (
    <div className="min-h-screen flex items-center justify-center text-agora-400">Loading…</div>
  )

  if (needsSetup) return (
    <Routes>
      <Route path="*" element={<SetupPage />} />
    </Routes>
  )

  return (
    <Routes>
      {/* Public landing — unauthenticated visitors see this at / */}
      <Route path="/"               element={<GuestOnly><LandingPage /></GuestOnly>} />
      <Route path="/login"          element={<GuestOnly><LoginPage /></GuestOnly>} />
      <Route path="/register"       element={<GuestOnly><RegisterPage /></GuestOnly>} />
      <Route path="/verify-email"   element={<VerifyEmailPage />} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route path="/change-password" element={<RequireAuth><ChangePasswordPage /></RequireAuth>} />
      <Route path="/privacy"        element={<PrivacyPage />} />
      <Route path="/terms"          element={<TermsPage />} />
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/feed"                    element={<FeedPage />} />
        <Route path="/profile/:username"       element={<ProfilePage />} />
        <Route path="/friends"                 element={<FriendsPage />} />
        <Route path="/search"                  element={<SearchPage />} />
        <Route path="/notifications"           element={<NotificationsPage />} />
        <Route path="/settings"                element={<SettingsPage />} />
        <Route path="/admin"                   element={<RequireAdmin><AdminPage /></RequireAdmin>} />
        <Route path="/post/:id"                element={<PostPage />} />
        <Route path="/discover"                element={<DiscoverPage />} />
        <Route path="/lists/:id"               element={<ListFeedPage />} />
        <Route path="/groups"                  element={<GroupsPage />} />
        <Route path="/groups/:slug"            element={<GroupPage />} />
        <Route path="/invite/:token"           element={<InvitePage />} />
        <Route path="/albums"                  element={<AlbumsPage />} />
        <Route path="/albums/:id"              element={<AlbumPage />} />
        <Route path="/messages"                element={<MessagesPage />} />
        <Route path="/messages/:convId"        element={<MessagesPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  )
}
