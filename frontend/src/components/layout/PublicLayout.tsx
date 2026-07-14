import { useState, useEffect } from 'react'
import { Link, Outlet } from 'react-router-dom'
import { Sun, Moon, Compass } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { instanceApi } from '../../api'

// Minimal chrome for logged-out visitors browsing public content — no
// notification/DM polling and no member-only nav links (see Layout.tsx for
// the authenticated equivalent).
export default function PublicLayout() {
  const [dark, setDark] = useState(() => localStorage.getItem('theme') === 'dark')

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    localStorage.setItem('theme', dark ? 'dark' : 'light')
  }, [dark])

  const { data: instanceData } = useQuery({
    queryKey: ['instance-info'],
    queryFn: () => instanceApi.getInfo().then(r => r.data),
    staleTime: 5 * 60_000,
  })
  const instanceName: string = instanceData?.instance_name || 'Agora'
  const logoUrl: string = instanceData?.logo_url || ''

  return (
    <div className="min-h-screen flex flex-col">
      <header className="flex items-center justify-between px-4 h-14 border-b border-agora-200 dark:border-agora-700 bg-white dark:bg-agora-900 sticky top-0 z-10">
        <Link to="/" className="flex items-center gap-2.5">
          <div className="w-8 h-8 rounded-lg bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
            {logoUrl
              ? <img src={logoUrl} alt={instanceName} className="w-full h-full object-cover" />
              : <span className="text-white font-bold text-sm">{instanceName[0]?.toUpperCase()}</span>
            }
          </div>
          <span className="font-bold text-agora-800 dark:text-agora-100 truncate">{instanceName}</span>
        </Link>
        <div className="flex items-center gap-1.5">
          <Link to="/explore"
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
            <Compass size={16} /> Explore
          </Link>
          <button onClick={() => setDark(d => !d)}
            className="p-1.5 rounded-lg text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
            {dark ? <Sun size={18} /> : <Moon size={18} />}
          </button>
          <Link to="/login" className="btn-secondary text-sm py-1.5 px-3">Sign in</Link>
          <Link to="/register" className="btn-primary text-sm py-1.5 px-3 hidden sm:inline-flex">Create account</Link>
        </div>
      </header>

      <main className="flex-1 mx-auto w-full max-w-2xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
