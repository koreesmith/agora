import { useState, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import api from '../../api'

export interface MentionUser {
  id: string
  username: string
  display_name: string
  avatar_url: string
  is_friend: boolean
  is_remote?: boolean
  remote_instance?: string
}

export interface MentionGroup {
  slug: string
  name: string
  avatar_url: string
}

export interface MentionPage {
  slug: string
  display_name: string
  avatar_url: string
}

export function useMentions() {
  const [mentionQuery, setMentionQuery] = useState('')
  const [mentionStart, setMentionStart] = useState(-1)
  const [showMentions, setShowMentions] = useState(false)
  const inputRef = useRef<HTMLTextAreaElement | HTMLInputElement>(null)

  // Unified search: users + groups + pages
  const { data } = useQuery({
    queryKey: ['mention-search', mentionQuery],
    queryFn: () => api.get('/mention-search', { params: { q: mentionQuery } }).then(r => r.data),
    enabled: showMentions,
    staleTime: 10_000,
  })
  const mentionUsers: MentionUser[]  = data?.users  || []
  const mentionGroups: MentionGroup[] = data?.groups || []
  const mentionPages: MentionPage[]  = data?.pages  || []

  const handleChange = (val: string, cursorPos: number) => {
    let i = cursorPos - 1
    while (i >= 0 && !/\s/.test(val[i])) i--
    const wordStart = i + 1
    const word = val.slice(wordStart, cursorPos)

    if (word.startsWith('@')) {
      setMentionQuery(word.slice(1))
      setMentionStart(wordStart)
      setShowMentions(true)
    } else {
      setShowMentions(false)
      setMentionQuery('')
    }
  }

  // Insert a user mention (@username) or a group/page tag (+slug)
  const insertMention = (content: string, setContent: (s: string) => void, tag: string) => {
    const before = content.slice(0, mentionStart)
    const after = content.slice(mentionStart + mentionQuery.length + 1)
    const newContent = before + tag + ' ' + after
    setContent(newContent)
    setShowMentions(false)
    setMentionQuery('')
    setTimeout(() => {
      const el = inputRef.current
      if (el) {
        const pos = (before + tag + ' ').length
        el.focus()
        el.setSelectionRange(pos, pos)
      }
    }, 0)
  }

  const dismiss = () => setShowMentions(false)

  return { mentionUsers, mentionGroups, mentionPages, showMentions, handleChange, insertMention, dismiss, inputRef }
}
