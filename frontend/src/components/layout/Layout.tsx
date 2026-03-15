import { useState, useEffect } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Home, Bell, Users, Search, Settings, Shield, LogOut, User, Menu, X, Sun, Moon, Compass, Users2, Images, MessageCircle } from 'lucide-react'
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

  const nav = [
    { to: '/',                          icon: Home,           label: 'Feed' },
    { to: '/notifications',             icon: Bell,           label: 'Notifications', badge: unread },
    { to: '/messages',                  icon: MessageCircle,  label: 'Messages',      badge: unreadDMs },
    { to: '/friends',                   icon: Users,          label: 'Friends' },
    { to: '/groups',                    icon: Users2,         label: 'Groups' },
    { to: '/albums',                    icon: Images,         label: 'Albums' },
    { to: '/discover',                  icon: Compass,        label: 'Find Friends' },
    { to: '/search',                    icon: Search,         label: 'Search' },
    { to: `/profile/${user?.username}`, icon: User,           label: 'Profile' },
    { to: '/settings',                  icon: Settings,       label: 'Settings' },
    ...(user?.role === 'admin' || user?.role === 'moderator'
      ? [{ to: '/admin', icon: Shield, label: 'Admin' }]
      : []),
  ]

  const isActive = (to: string) =>
    to === '/' ? location.pathname === '/' : location.pathname.startsWith(to)

  const SidebarContent = () => (
    <div className="flex flex-col h-full">
      {/* Logo / instance name */}
      <Link to="/" onClick={() => setMobileOpen(false)}
        className="flex items-center gap-3 px-4 py-5 mb-2">
        <div className="w-9 h-9 rounded-xl bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
          {logoUrl
            ? <img src={logoUrl} alt={instanceName} className="w-full h-full object-cover" />
            : <span className="text-white font-bold text-base">{instanceName[0]?.toUpperCase()}</span>
          }
        </div>
        <span className="font-bold text-lg text-agora-800 dark:text-agora-100 truncate">{instanceName}</span>
      </Link>

      {/* Nav links */}
      <nav className="flex-1 px-2 space-y-0.5">
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

      {/* Bottom actions */}
      <div className="px-2 pb-4 pt-2 border-t border-agora-100 dark:border-agora-700 space-y-0.5">
        <button onClick={() => setDark(d => !d)}
          className="flex items-center gap-3 px-3 py-2.5 w-full rounded-lg text-sm font-medium text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
          {dark ? <Sun size={18} /> : <Moon size={18} />}
          {dark ? 'Light mode' : 'Dark mode'}
        </button>
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

      {/* Main content */}
      <div className="flex-1 md:ml-60">
        {/* Mobile topbar */}
        <header className="md:hidden flex items-center justify-between px-4 h-14 border-b border-agora-200 dark:border-agora-700 bg-white dark:bg-agora-900 sticky top-0 z-10">
          <button onClick={() => setMobileOpen(true)} className="text-agora-600 dark:text-agora-300">
            <Menu size={22} />
          </button>
          <span className="font-bold text-agora-800 dark:text-agora-100">{instanceName}</span>
          <div className="w-6" />
        </header>

        <main className="max-w-2xl mx-auto px-4 py-6">
          <Outlet />
        </main>
      </div>
      <ChatWindows />
    </div>
  )
}
