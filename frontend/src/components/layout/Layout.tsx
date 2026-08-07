import { useState, useEffect } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Home, Bell, Users, Settings, Shield, LogOut, User, Menu, X, Sun, Moon, Users2, MessageCircle, Rss, BookOpen, HelpCircle, Sparkles } from 'lucide-react'
import WhatsNewModal from '../common/WhatsNewModal'
import SearchBar from './SearchBar'
import { useAuthStore } from '../../store/auth'
import { notificationsApi, instanceApi, dmApi } from '../../api'
import { useQuery } from '@tanstack/react-query'
import ChatWindows from '../common/ChatWindows'

export default function Layout() {
  const { user, logout } = useAuthStore()
  const location = useLocation()
  const navigate = useNavigate()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [dark, setDark] = useState(() => localStorage.getItem('theme') === 'dark')
  const [showWhatsNew, setShowWhatsNew] = useState(false)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    localStorage.setItem('theme', dark ? 'dark' : 'light')
  }, [dark])

  const { data: unreadData } = useQuery({
    queryKey: ['unread-count'],
    queryFn: () => notificationsApi.unreadCount().then(r => r.data),
    refetchInterval: 30_000,
  })
  const unread: number = unreadData?.count ?? 0

  const { data: convsData } = useQuery({
    queryKey: ['conversations'],
    queryFn: () => dmApi.listConversations().then(r => r.data),
    refetchInterval: 30_000,
  })
  const unreadDMs: number = (convsData?.conversations || []).reduce((sum: number, c: any) => sum + (c.unread_count || 0), 0)

  const { data: instanceData } = useQuery({
    queryKey: ['instance-info'],
    queryFn: () => instanceApi.getInfo().then(r => r.data),
    staleTime: 5 * 60_000,
  })
  const instanceName: string = instanceData?.instance_name || 'Agora'
  const logoUrl: string = instanceData?.logo_url || ''


  // AGORA-335: kept to the destinations that are genuinely top-level. Three
  // former entries now live where their content already does, so the routes are
  // unchanged and only the duplicate signposting is gone:
  //   Photo Albums  -> the Photos tab on a profile, which already lists albums
  //                    and already links to /albums to manage them
  //   Find Friends  -> an action on Connections, which is where you go to look
  //                    at the people you know
  //   Invite a Friend -> the same
  const nav = [
    { to: '/',                          icon: Home,           label: 'Feed' },
    { to: '/notifications',             icon: Bell,           label: 'Notifications', badge: unread },
    { to: '/messages',                  icon: MessageCircle,  label: 'Messages',      badge: unreadDMs },
    { to: '/connections',               icon: Users,          label: 'Connections' },
    { to: '/groups',                    icon: Users2,         label: 'Groups' },
    { to: '/pages',                     icon: BookOpen,       label: 'Pages' },
    { to: '/my-feeds',                  icon: Rss,            label: 'My Feeds' },
    { to: `/profile/${user?.username}`, icon: User,           label: 'Profile' },
    { to: '/settings',                  icon: Settings,       label: 'Settings' },
    ...(user?.role === 'admin' || user?.role === 'moderator'
      ? [{ to: '/admin', icon: Shield, label: 'Admin' }]
      : []),
  ]

  const isActive = (to: string) =>
    to === '/' ? location.pathname === '/' : location.pathname.startsWith(to)

  // Shared by the topbar and <main> so the search field sits directly over the
  // content column instead of drifting out of line on the wider DM view.
  const contentWidth = location.pathname.startsWith('/messages') ? 'max-w-5xl' : 'max-w-2xl'

  const SidebarContent = () => (
    <div className="flex flex-col h-full">
      {/* Logo / instance name */}
      <Link to="/feed" onClick={() => setMobileOpen(false)}
        className="flex items-center gap-3 px-4 py-5 mb-2">
        <div className="w-9 h-9 rounded-xl bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
          {logoUrl
            ? <img src={logoUrl} alt={instanceName} className="w-full h-full object-cover" />
            : <span className="text-white font-bold text-base">{instanceName[0]?.toUpperCase()}</span>
          }
        </div>
        <span className="font-bold text-lg text-agora-800 dark:text-agora-100 truncate">{instanceName}</span>
      </Link>

      {/* Nav links.
          overflow-y-auto + min-h-0 is load-bearing, not cosmetic: without it a
          nav taller than the viewport pushes the block below (which holds Sign
          out) off the bottom of the screen with no way to scroll to it, so on a
          short window there was simply no way to log out. flex-1 alone does not
          constrain a flex child's height; min-h-0 is what lets it shrink and
          scroll instead of growing past its container. */}
      <nav className="flex-1 min-h-0 overflow-y-auto px-2 space-y-0.5">
        {nav.map(({ to, icon: Icon, label, badge }) => (
          <Link key={to} to={to} onClick={() => setMobileOpen(false)}
            className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors relative ${
              isActive(to)
                ? 'bg-agora-100 dark:bg-agora-700 text-agora-900 dark:text-agora-50'
                : 'text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800'
            }`}>
            <Icon size={18} />
            {label}
            {badge != null && badge > 0 && (
              <span className="ml-auto bg-red-500 text-white text-xs rounded-full min-w-[18px] h-[18px] flex items-center justify-center px-1">
                {badge > 9 ? '9+' : badge}
              </span>
            )}
          </Link>
        ))}
      </nav>

      {/* Bottom actions. AGORA-335: the three incidental ones collapse to an
          icon row so this block costs one line instead of three, which keeps
          Sign out visible without depending on how tall the nav above happens
          to be. Each keeps a title and an aria-label, since an icon alone is
          not a label. */}
      <div className="px-2 pb-4 pt-2 border-t border-agora-100 dark:border-agora-700 space-y-1 flex-shrink-0">
        <div className="flex items-center gap-1">
          <button onClick={() => setDark(d => !d)}
            title={dark ? 'Light mode' : 'Dark mode'}
            aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
            className="flex-1 flex items-center justify-center py-2 rounded-lg text-agora-500 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
            {dark ? <Sun size={18} /> : <Moon size={18} />}
          </button>
          {/* AGORA-132: Help & What's New */}
          <a href="/docs#user/index" target="_blank" rel="noopener noreferrer"
            title="Help & docs" aria-label="Help and docs"
            className="flex-1 flex items-center justify-center py-2 rounded-lg text-agora-500 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
            <HelpCircle size={18} />
          </a>
          <button onClick={() => setShowWhatsNew(true)}
            title="What's new" aria-label="What's new"
            className="flex-1 flex items-center justify-center py-2 rounded-lg text-agora-500 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
            <Sparkles size={18} />
          </button>
        </div>
        <button onClick={() => { logout(); navigate('/login') }}
          className="flex items-center gap-3 px-3 py-2.5 w-full rounded-lg text-sm font-medium text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors">
          <LogOut size={18} />
          Sign out
        </button>
      </div>
    </div>
  )

  return (
    <div className="min-h-screen flex">
      {/* Desktop sidebar */}
      <aside className="hidden md:flex flex-col w-60 border-r border-agora-200 dark:border-agora-700 bg-white dark:bg-agora-900 fixed h-full z-10">
        <SidebarContent />
      </aside>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div className="md:hidden fixed inset-0 z-20 flex">
          <div className="fixed inset-0 bg-black/40" onClick={() => setMobileOpen(false)} />
          <aside className="relative w-60 bg-white dark:bg-agora-900 h-full z-30 shadow-xl">
            <button onClick={() => setMobileOpen(false)}
              className="absolute top-4 right-4 text-agora-400 hover:text-agora-700">
              <X size={20} />
            </button>
            <SidebarContent />
          </aside>
        </div>
      )}

      {/* Main content. min-w-0 because a flex item defaults to min-width:auto,
          which makes this column refuse to shrink below its widest content:
          anything too wide pushed the whole page sideways instead of scrolling
          or wrapping inside its own box. Both the topbar search field and the
          feed pill row (AGORA-303) rely on this column being constrained. */}
      <div className="flex-1 min-w-0 md:ml-60">
        {/* Topbar — search lives here now (AGORA-304), so unlike the mobile-only
            bar it replaced it renders at every breakpoint; desktop previously had
            no header at all. The instance name is gone from it: on mobile the
            search field needs the width, and the name is still in the sidebar
            header one tap away. */}
        <header className="sticky top-0 z-10 border-b border-agora-200 dark:border-agora-700 bg-white dark:bg-agora-900">
          <div className={`mx-auto flex items-center gap-3 px-4 h-14 ${contentWidth}`}>
            <button onClick={() => setMobileOpen(true)}
              aria-label={`Open ${instanceName} menu`}
              className="md:hidden text-agora-600 dark:text-agora-300 flex-shrink-0">
              <Menu size={22} />
            </button>
            <SearchBar menuOpen={mobileOpen} />
          </div>
        </header>

        <main className={`mx-auto px-4 py-6 ${contentWidth}`}>
          <Outlet />
        </main>
      </div>
      <ChatWindows />
      {/* AGORA-132: What's New modal — auto-shows on first login; also manually triggerable */}
      <WhatsNewModal forceShow={showWhatsNew} onClose={() => setShowWhatsNew(false)} />
    </div>
  )
}
