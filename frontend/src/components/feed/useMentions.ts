import { useState, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { usersApi } from '../../api'

export interface MentionUser {
  id: string
  username: string
  display_name: string
  avatar_url: string
  is_friend: boolean
  is_remote?: boolean
  remote_instance?: string
}

export function useMentions() {
  const [mentionQuery, setMentionQuery] = useState('')
  const [mentionStart, setMentionStart] = useState(-1)
  const [showMentions, setShowMentions] = useState(false)
  const inputRef = useRef<HTMLTextAreaElement | HTMLInputElement>(null)

  const { data } = useQuery({
    queryKey: ['mention-search', mentionQuery],
    queryFn: () => usersApi.mentionSearch(mentionQuery).then(r => r.data),
    enabled: showMentions,   // empty query returns friends as suggestions
    staleTime: 10_000,
  })
  const mentionUsers: MentionUser[] = data?.users || []

  const handleChange = (val: string, cursorPos: number) => {
    // Walk back from cursor to find word start
    let i = cursorPos - 1
    while (i >= 0 && !/\s/.test(val[i])) i--
    const wordStart = i + 1
    const word = val.slice(wordStart, cursorPos)

    // Show on '@' alone (empty query → friend suggestions) or '@word' (filtered)
    if (word.startsWith('@')) {
      setMentionQuery(word.slice(1))
      setMentionStart(wordStart)
      setShowMentions(true)
    } else {
      setShowMentions(false)
      setMentionQuery('')
    }
  }

  const insertMention = (content: string, setContent: (s: string) => void, username: string) => {
    const before = content.slice(0, mentionStart)
    const after = content.slice(mentionStart + mentionQuery.length + 1)
    const newContent = before + '@' + username + ' ' + after
    setContent(newContent)
    setShowMentions(false)
    setMentionQuery('')
    setTimeout(() => {
      const el = inputRef.current
      if (el) {
        const pos = (before + '@' + username + ' ').length
        el.focus()
        el.setSelectionRange(pos, pos)
      }
    }, 0)
  }

  const dismiss = () => setShowMentions(false)

  return { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef }
}
