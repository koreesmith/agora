import { create } from 'zustand'

interface OpenChat {
  convId: string
  minimized: boolean
}

interface ChatStore {
  openChats: OpenChat[]
  openChat: (convId: string) => void
  closeChat: (convId: string) => void
  toggleMinimize: (convId: string) => void
  minimizeAll: () => void
}

export const useChatStore = create<ChatStore>((set) => ({
  openChats: [],

  openChat: (convId) => set((state) => {
    const exists = state.openChats.find(c => c.convId === convId)
    if (exists) {
      // Un-minimize if minimized
      return { openChats: state.openChats.map(c => c.convId === convId ? { ...c, minimized: false } : c) }
    }
    // Max 3 open at once — close oldest if needed
    const chats = state.openChats.length >= 3
      ? state.openChats.slice(1)
      : state.openChats
    return { openChats: [...chats, { convId, minimized: false }] }
  }),

  closeChat: (convId) => set((state) => ({
    openChats: state.openChats.filter(c => c.convId !== convId),
  })),

  toggleMinimize: (convId) => set((state) => ({
    openChats: state.openChats.map(c => c.convId === convId ? { ...c, minimized: !c.minimized } : c),
  })),

  minimizeAll: () => set((state) => ({
    openChats: state.openChats.map(c => ({ ...c, minimized: true })),
  })),
}))
