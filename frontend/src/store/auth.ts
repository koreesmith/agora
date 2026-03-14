import { create } from 'zustand'

export interface User {
  id: string
  username: string
  email: string
  display_name: string
  bio: string
  avatar_url: string
  cover_url: string
  cover_position: string
  location: string
  website: string
  role: 'user' | 'moderator' | 'admin'
  profile_private: boolean
}

interface AuthState {
  user: User | null
  token: string | null
  isAuthenticated: boolean
  setAuth: (user: User, token: string) => void
  updateUser: (updates: Partial<User>) => void
  logout: () => void
}

const loadUser = (): User | null => {
  try {
    const s = localStorage.getItem('agora_user')
    return s ? JSON.parse(s) : null
  } catch { return null }
}

export const useAuthStore = create<AuthState>((set) => ({
  user: loadUser(),
  token: localStorage.getItem('agora_token'),
  isAuthenticated: !!localStorage.getItem('agora_token'),

  setAuth: (user, token) => {
    localStorage.setItem('agora_token', token)
    localStorage.setItem('agora_user', JSON.stringify(user))
    set({ user, token, isAuthenticated: true })
  },

  updateUser: (updates) => set((state) => {
    if (!state.user) return state
    const updated = { ...state.user, ...updates }
    localStorage.setItem('agora_user', JSON.stringify(updated))
    return { user: updated }
  }),

  logout: () => {
    localStorage.removeItem('agora_token')
    localStorage.removeItem('agora_user')
    set({ user: null, token: null, isAuthenticated: false })
  },
}))
