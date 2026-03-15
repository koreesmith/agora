import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { friendsApi } from '../../api'
import { X, Plus, Check } from 'lucide-react'

interface Props {
  friend: { id: string; display_name?: string; username: string; avatar_url?: string }
  onClose: () => void
}

export default function FriendListModal({ friend, onClose }: Props) {
  const qc = useQueryClient()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [newListName, setNewListName] = useState('')
  const [saving, setSaving] = useState(false)

  const { data: listsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
  })
  const lists: any[] = listsData?.groups || []

  const createList = useMutation({
    mutationFn: (name: string) => friendsApi.createGroup(name),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['friend-groups'] })
      // Auto-select the newly created list
      const newId = res.data?.id
      if (newId) setSelected(s => new Set([...s, newId]))
      setNewListName('')
    },
  })

  const handleToggle = (id: string) => {
    setSelected(s => {
      const next = new Set(s)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const handleSave = async () => {
    if (selected.size === 0) { onClose(); return }
    setSaving(true)
    try {
      await Promise.all([...selected].map(listID =>
        friendsApi.addToGroup(listID, friend.id)
      ))
      qc.invalidateQueries({ queryKey: ['friend-groups'] })
    } finally {
      setSaving(false)
      onClose()
    }
  }

  const handleCreateAndSelect = () => {
    const name = newListName.trim()
    if (!name) return
    createList.mutate(name)
  }

  const displayName = friend.display_name || friend.username

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60"
      onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-sm"
        onClick={e => e.stopPropagation()}>

        {/* Header */}
        <div className="flex items-center justify-between px-4 pt-4 pb-3 border-b border-agora-100 dark:border-agora-700">
          <div>
            <h3 className="font-semibold text-agora-900 dark:text-agora-100">Add to friend lists</h3>
            <p className="text-xs text-agora-400 mt-0.5">Add {displayName} to lists, or skip for now</p>
          </div>
          <button onClick={onClose} className="text-agora-400 hover:text-agora-600 transition-colors">
            <X size={18} />
          </button>
        </div>

        {/* Friend avatar + name */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-agora-100 dark:border-agora-700">
          <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
            {friend.avatar_url
              ? <img src={friend.avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-600">
                  {displayName[0].toUpperCase()}
                </span>
            }
          </div>
          <div>
            <p className="text-sm font-medium text-agora-900 dark:text-agora-100">{displayName}</p>
            <p className="text-xs text-agora-400">@{friend.username}</p>
          </div>
        </div>

        {/* List selection */}
        <div className="px-4 py-3 max-h-52 overflow-y-auto space-y-1">
          {lists.length === 0 && !createList.isPending && (
            <p className="text-sm text-agora-400 text-center py-3">No lists yet — create one below</p>
          )}
          {lists.map((list: any) => {
            const isSelected = selected.has(list.id)
            return (
              <button
                key={list.id}
                onClick={() => handleToggle(list.id)}
                className={`w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors ${
                  isSelected
                    ? 'bg-agora-100 dark:bg-agora-700 text-agora-900 dark:text-agora-100'
                    : 'hover:bg-agora-50 dark:hover:bg-agora-700/50 text-agora-700 dark:text-agora-300'
                }`}
              >
                <div className={`w-4 h-4 rounded border-2 flex items-center justify-center flex-shrink-0 transition-colors ${
                  isSelected ? 'bg-agora-600 border-agora-600' : 'border-agora-300 dark:border-agora-500'
                }`}>
                  {isSelected && <Check size={10} className="text-white" strokeWidth={3} />}
                </div>
                <span className="text-sm flex-1 truncate">{list.name}</span>
                <span className="text-xs text-agora-400 flex-shrink-0">{list.member_count} {list.member_count === 1 ? 'person' : 'people'}</span>
              </button>
            )
          })}
        </div>

        {/* Create new list */}
        <div className="px-4 pb-3 border-t border-agora-100 dark:border-agora-700 pt-3">
          <div className="flex gap-2">
            <input
              className="input flex-1 text-sm"
              autoComplete="off"
              placeholder="New list name…"
              value={newListName}
              onChange={e => setNewListName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleCreateAndSelect()}
            />
            <button
              onClick={handleCreateAndSelect}
              disabled={!newListName.trim() || createList.isPending}
              className="btn-secondary px-3 flex-shrink-0"
              title="Create list"
            >
              <Plus size={15} />
            </button>
          </div>
        </div>

        {/* Footer actions */}
        <div className="flex gap-2 px-4 pb-4">
          <button onClick={onClose} className="btn-secondary flex-1 text-sm">
            Skip
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="btn-primary flex-1 text-sm"
          >
            {saving ? 'Saving…' : selected.size > 0 ? `Add to ${selected.size} list${selected.size > 1 ? 's' : ''}` : 'Done'}
          </button>
        </div>
      </div>
    </div>
  )
}
