import { useEffect, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuthStore } from './store/auth'
import api from './api'
import Layout from './components/layout/Layout'
import PublicLayout from './components/layout/PublicLayout'

import SetupPage          from './pages/SetupPage'
import LandingPage        from './pages/LandingPage'
import LoginPage          from './pages/LoginPage'
import RegisterPage       from './pages/RegisterPage'
import VerifyEmailPage       from './pages/VerifyEmailPage'
import VerifyEmailChangePage from './pages/VerifyEmailChangePage'
import ResetPasswordPage  from './pages/ResetPasswordPage'
import ChangePasswordPage from './pages/ChangePasswordPage'
import PrivacyPage        from './pages/PrivacyPage'
import TermsPage          from './pages/TermsPage'
import SupportPage        from './pages/SupportPage'
import FeedPage           from './pages/FeedPage'
import ProfilePage        from './pages/ProfilePage'
import ConnectionsPage    from './pages/ConnectionsPage'
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
import InviteFriendPage   from './pages/InviteFriendPage'
import CustomFeedsPage    from './pages/CustomFeedsPage'
import BookmarksPage      from './pages/BookmarksPage'
import CustomFeedViewPage from './pages/CustomFeedViewPage'
import CreatePagePage     from './pages/CreatePagePage'
import PagesPage         from './pages/PagesPage'
import PageProfilePage   from './pages/PageProfilePage'
import PageSettingsPage  from './pages/PageSettingsPage'
import ExplorePage       from './pages/ExplorePage'
import ReelsPage         from './pages/ReelsPage'
import WelcomePage       from './pages/WelcomePage'

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

// Renders the full member Layout when signed in, or the minimal PublicLayout
// for guests — used by routes reachable both logged in and logged out
// (public profiles, post permalinks, explore).
function AuthAwareLayout() {
  const { isAuthenticated } = useAuthStore()
  return isAuthenticated ? <Layout /> : <PublicLayout />
}

function AppRoutes() {
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  // AGORA-458: the first request has a time limit. A server that accepts it and
  // never answers used to leave the page on "Loading…" for ever; now the page
  // says so and keeps trying.
  const [unreachable, setUnreachable] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    api.get('/setup', { timeout: 10000 })
      .then(r => { if (!cancelled) { setUnreachable(false); setNeedsSetup(r.data.needs_setup) } })
      .catch((e: any) => {
        if (cancelled) return
        // The server answered with an error: carry on as before. No answer in
        // time, or no connection: say so and try again.
        if (e?.response) { setNeedsSetup(false); return }
        setUnreachable(true)
        setTimeout(() => { if (!cancelled) setAttempt(n => n + 1) }, 5000)
      })
    return () => { cancelled = true }
  }, [attempt])

  if (needsSetup === null && unreachable) return (
    <div className="min-h-screen flex flex-col items-center justify-center gap-3 px-6 text-center text-agora-500">
      <p className="font-medium text-agora-700 dark:text-agora-200">Can't reach the server right now.</p>
      <p className="text-sm">It may be restarting. This page will keep trying.</p>
      <button onClick={() => setAttempt(n => n + 1)} className="btn-primary text-sm">Try again now</button>
    </div>
  )

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
      <Route path="/verify-email"        element={<VerifyEmailPage />} />
      <Route path="/verify-email-change" element={<VerifyEmailChangePage />} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route path="/change-password" element={<RequireAuth><ChangePasswordPage /></RequireAuth>} />
      {/* AGORA-388: the getting started wizard is a full page of its own,
          outside the member Layout, which is what redirects new accounts here. */}
      <Route path="/welcome"        element={<RequireAuth><WelcomePage /></RequireAuth>} />
      <Route path="/privacy"        element={<PrivacyPage />} />
      <Route path="/terms"          element={<TermsPage />} />
      <Route path="/support"        element={<SupportPage />} />
      {/* Guest-accessible: public profiles, post permalinks, and the explore
          feed render with the full member Layout when signed in, or the
          minimal PublicLayout for guests. */}
      <Route element={<AuthAwareLayout />}>
        <Route path="/profile/:username"       element={<ProfilePage />} />
        <Route path="/post/:id"                element={<PostPage />} />
        <Route path="/explore"                 element={<ExplorePage />} />
      </Route>
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/feed"                    element={<FeedPage />} />
        <Route path="/reels"                   element={<ReelsPage />} />
        <Route path="/connections"             element={<ConnectionsPage />} />
        {/* Old routes from before the Friends/Fediverse merge — kept as
            redirects so existing links and bookmarks still land somewhere. */}
        <Route path="/friends"                 element={<Navigate to="/connections" replace />} />
        <Route path="/fediverse"               element={<Navigate to="/connections?tab=fediverse" replace />} />
        <Route path="/search"                  element={<SearchPage />} />
        <Route path="/notifications"           element={<NotificationsPage />} />
        <Route path="/settings"                element={<SettingsPage />} />
        <Route path="/admin"                   element={<RequireAdmin><AdminPage /></RequireAdmin>} />
        <Route path="/discover"                element={<DiscoverPage />} />
        <Route path="/lists/:id"               element={<ListFeedPage />} />
        <Route path="/groups"                  element={<GroupsPage />} />
        <Route path="/groups/:slug"            element={<GroupPage />} />
        <Route path="/invite/:token"           element={<InvitePage />} />
        <Route path="/albums"                  element={<AlbumsPage />} />
        <Route path="/albums/:id"              element={<AlbumPage />} />
        <Route path="/messages"                element={<MessagesPage />} />
        <Route path="/messages/:convId"        element={<MessagesPage />} />
        <Route path="/invite-friend"           element={<InviteFriendPage />} />
        <Route path="/my-feeds"               element={<CustomFeedsPage />} />
        <Route path="/bookmarks"              element={<BookmarksPage />} />
        <Route path="/my-feeds/:id"           element={<CustomFeedViewPage />} />
        <Route path="/pages"                  element={<PagesPage />} />
        <Route path="/pages/new"              element={<CreatePagePage />} />
        <Route path="/pages/:slug/settings"   element={<PageSettingsPage />} />
        <Route path="/pages/:slug"            element={<PageProfilePage />} />
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
