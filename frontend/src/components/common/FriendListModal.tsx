import { useState, useEffect } from 'react'
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
  // The membership state as of when the modal opened, so Save can diff
  // against it (add newly-checked lists, remove newly-unchecked ones)
  // instead of blindly re-adding every currently-checked list every time.
  const [initialSelected, setInitialSelected] = useState<Set<string>>(new Set())
  const [membershipLoaded, setMembershipLoaded] = useState(false)
  const [newListName, setNewListName] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const { data: listsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listFriendLists().then(r => r.data),
  })
  const lists: any[] = listsData?.groups || []

  // Load which of these lists this account is already a member of, so the
  // checkboxes reflect reality instead of always starting unchecked —
  // previously this modal had no idea whether the account it was opened
  // for was already on any list.
  useEffect(() => {
    if (!listsData) return
    let cancelled = false
    setMembershipLoaded(false)
    Promise.all(
      lists.map(list =>
        friendsApi.listFriendListMembers(list.id).then(r => ({
          listID: list.id,
          isMember: (r.data?.members || []).some((m: any) => m.id === friend.id),
        }))
      )
    ).then(results => {
      if (cancelled) return
      const memberOf = new Set(results.filter(r => r.isMember).map(r => r.listID))
      setSelected(memberOf)
      setInitialSelected(memberOf)
      setMembershipLoaded(true)
    })
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listsData, friend.id])

  const createList = useMutation({
    mutationFn: (name: string) => friendsApi.createFriendList(name),
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

  const toAdd = [...selected].filter(id => !initialSelected.has(id))
  const toRemove = [...initialSelected].filter(id => !selected.has(id))
  const hasChanges = toAdd.length > 0 || toRemove.length > 0

  const handleSave = async () => {
    if (!hasChanges) { onClose(); return }
    setSaving(true)
    setError('')
    try {
      await Promise.all([
        ...toAdd.map(listID => friendsApi.addToFriendList(listID, friend.id)),
        ...toRemove.map(listID => friendsApi.removeFromFriendList(listID, friend.id)),
      ])
      qc.invalidateQueries({ queryKey: ['friend-groups'] })
      for (const listID of [...toAdd, ...toRemove]) {
        qc.invalidateQueries({ queryKey: ['list-members', listID] })
      }
      onClose()
    } catch (err: any) {
      setError(err.response?.data?.error || 'Could not update lists — please try again.')
    } finally {
      setSaving(false)
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
          {lists.length > 0 && !membershipLoaded && (
            <p className="text-sm text-agora-400 text-center py-3">Loading current lists…</p>
          )}
          {membershipLoaded && lists.map((list: any) => {
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

        {error && <p className="text-sm text-red-500 px-4 pb-2">{error}</p>}

        {/* Footer actions */}
        <div className="flex gap-2 px-4 pb-4">
          <button onClick={onClose} className="btn-secondary flex-1 text-sm">
            Skip
          </button>
          <button
            onClick={handleSave}
            disabled={saving || !membershipLoaded}
            className="btn-primary flex-1 text-sm"
          >
            {saving ? 'Saving…' : hasChanges ? 'Save changes' : 'Done'}
          </button>
        </div>
      </div>
    </div>
  )
}
