import { create } from 'zustand'

// AGORA-440: whether Reels plays with the sound off. Browsers only allow
// autoplay with sound after the person has interacted with the page, so it
// starts muted, and unmuting once sticks for the rest of the session instead
// of asking on every video.
const KEY = 'agora_reels_muted'

function initial(): boolean {
  try { return sessionStorage.getItem(KEY) !== 'false' } catch { return true }
}

interface ReelsState {
  muted: boolean
  setMuted: (m: boolean) => void
}

export const useReelsStore = create<ReelsState>((set) => ({
  muted: initial(),
  setMuted: (muted) => {
    try { sessionStorage.setItem(KEY, String(muted)) } catch { /* private mode */ }
    set({ muted })
  },
}))
