import { useState, useEffect } from 'react'
import { Outlet, Link, useLocation, useNavigate } from 'react-router-dom'
import {
  Home, Bell, Users, Search, Settings, Shield, LogOut,
  Menu, X, Sun, Moon, User
} from 'lucide-react'
import { useAuthStore } from '../../store/auth'
import { notificationsApi, instanceApi } from '../../api'
import { useQuery } from '@tanstack/react-query'

export default function Layout() {
  const { user, logout } = useAuthStore()
  const location = useLocation()
  const navigate = useNavigate()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [dark, setDark] = useState(() => localStorage.getItem('theme') === 'dark')

  const { data: unreadData } = useQuery({
    queryKey: ['unread-count'],
    queryFn: () => notificationsApi.unreadCount().then(r => r.data),
    refetchInterval: 30_000,
  })

  const { data: instanceData } = useQuery({
    queryKey: ['instance-info'],
    queryFn: () => instanceApi.getInfo().then(r => r.data),
    staleTime: 5 * 60_000,
  })
  const instanceName: string = instanceData?.instance_name || 'Agora'
  const logoUrl: string = instanceData?.logo_url || ''

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    localStorage.setItem('theme', dark ? 'dark' : 'light')
  }, [dark])

  const nav = [
    { href: '/', icon: Home, label: 'Feed' },
    { href: '/notifications', icon: Bell, label: 'Notifications', badge: unreadData?.count },
    { href: '/friends', icon: Users, label: 'Friends' },
    { href: '/search', icon: Search, label: 'Search' },
    { href: `/profile/${user?.username}`, icon: User, label: 'Profile' },
    { href: '/settings', icon: Settings, label: 'Settings' },
    ...(user?.role === 'admin' || user?.role === 'moderator'
      ? [{ href: '/admin', icon: Shield, label: 'Admin' }]
      : []),
  ]

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  const Sidebar = ({ mobile = false }) => (
    <div className={`flex flex-col h-full ${mobile ? '' : 'py-4'}`}>
      {/* Logo */}
      <div className="px-4 py-3 mb-2">
        <Link to="/" className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-lg bg-agora-700 flex items-center justify-center overflow-hidden">
            {logoUrl
              ? <img src={logoUrl} alt={instanceName} className="w-full h-full object-cover" />
              : <span className="text-white font-bold text-sm">{instanceName[0]?.toUpperCase()}</span>
            }
          </div>
          <span className="font-bold text-lg text-agora-800 dark:text-agora-100">{instanceName}</span>
        </Link>
      </div>

      {/* Nav */}
      <nav className="flex-1 px-2 space-y-0.5">
        {nav.map(({ href, icon: Icon, label, badge }) => {
          const active = location.pathname === href ||
            (href !== '/' && location.pathname.startsWith(href))
          return (
            <Link
              key={href}
              to={href}
              onClick={() => setSidebarOpen(false)}
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors relative ${
                active
                  ? 'bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100'
                  : 'text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-800'
              }`}
            >
              <Icon size={18} />
              {label}
              {badge ? (
                <span className="ml-auto bg-red-500 text-white text-xs rounded-full w-5 h-5 flex items-center justify-center">
                  {badge > 9 ? '9+' : badge}
                </span>
              ) : null}
            </Link>
          )
        })}
      </nav>

      {/* User + controls */}
      <div className="px-2 pt-2 border-t border-gray-200 dark:border-agora-700 space-y-1">
        <button
          onClick={() => setDark(!dark)}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors"
        >
          {dark ? <Sun size={18} /> : <Moon size={18} />}
          {dark ? 'Light mode' : 'Dark mode'}
        </button>
        <button
          onClick={handleLogout}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
        >
          <LogOut size={18} />
          Sign out
        </button>
        <div className="flex items-center gap-2 px-3 py-2">
          {user?.avatar_url ? (
            <img src={user.avatar_url} alt="" className="w-8 h-8 rounded-full object-cover" />
          ) : (
            <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 flex items-center justify-center text-xs font-bold text-agora-700 dark:text-agora-200">
              {user?.username?.[0]?.toUpperCase()}
            </div>
          )}
          <div className="min-w-0">
            <div className="text-sm font-medium text-agora-800 dark:text-agora-100 truncate">{user?.display_name}</div>
            <div className="text-xs text-agora-500 truncate">@{user?.username}</div>
          </div>
        </div>
      </div>
    </div>
  )

  return (
    <div className="min-h-screen flex bg-gray-50 dark:bg-agora-900">
      {/* Desktop Sidebar */}
      <aside className="hidden md:flex flex-col w-60 bg-white dark:bg-agora-800 border-r border-gray-200 dark:border-agora-700 fixed h-full z-20">
        <Sidebar />
      </aside>

      {/* Mobile sidebar overlay */}
      {sidebarOpen && (
        <div className="md:hidden fixed inset-0 z-40">
          <div className="absolute inset-0 bg-black/50" onClick={() => setSidebarOpen(false)} />
          <aside className="absolute left-0 top-0 bottom-0 w-64 bg-white dark:bg-agora-800 shadow-xl">
            <div className="flex justify-end p-3">
              <button onClick={() => setSidebarOpen(false)}>
                <X size={20} />
              </button>
            </div>
            <Sidebar mobile />
          </aside>
        </div>
      )}

      {/* Mobile top bar */}
      <div className="md:hidden fixed top-0 left-0 right-0 z-30 bg-white dark:bg-agora-800 border-b border-gray-200 dark:border-agora-700 flex items-center px-4 h-14">
        <button onClick={() => setSidebarOpen(true)} className="mr-3">
          <Menu size={20} />
        </button>
        <span className="font-bold text-agora-800 dark:text-agora-100">Agora</span>
        {unreadData?.count ? (
          <span className="ml-auto bg-red-500 text-white text-xs rounded-full w-5 h-5 flex items-center justify-center">
            {unreadData.count > 9 ? '9+' : unreadData.count}
          </span>
        ) : null}
      </div>

      {/* Main content */}
      <main className="flex-1 md:ml-60 pt-14 md:pt-0 min-h-screen">
        <div className="max-w-2xl mx-auto px-4 py-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
