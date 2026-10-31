import { useMentions } from './useMentions'
import MentionDropdown from './MentionDropdown'

// AGORA-418: a textarea with the @mention / +tag dropdown the new-post and
// comment boxes already have, for the boxes that went without it (editing a
// post, and the commentary on a repost), where an @name stayed plain text.
export default function MentionTextarea({ value, onChange, className, rows, placeholder, autoFocus }: {
  value: string
  onChange: (value: string) => void
  className?: string
  rows?: number
  placeholder?: string
  autoFocus?: boolean
}) {
  const { mentionUsers, mentionGroups, mentionPages, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()
  return (
    <div className="relative">
      <textarea
        ref={inputRef as React.RefObject<HTMLTextAreaElement>}
        className={className}
        rows={rows}
        placeholder={placeholder}
        autoComplete="off"
        autoFocus={autoFocus}
        value={value}
        onChange={e => {
          onChange(e.target.value)
          handleChange(e.target.value, e.target.selectionStart ?? e.target.value.length)
        }}
        onKeyDown={e => { if (e.key === 'Escape' && showMentions) { e.stopPropagation(); dismiss() } }}
      />
      {showMentions && (
        <MentionDropdown users={mentionUsers} groups={mentionGroups} pages={mentionPages}
          onSelect={tag => insertMention(value, onChange, tag)} />
      )}
    </div>
  )
}
