import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { X, Plus, Trash2, Sparkles, RotateCcw } from 'lucide-react'
import { customFeedsApi, friendsApi, groupsApi, pagesApi, interactionsApi, federationApi } from '../../api'

interface Filter {
  filter_type: string
  value: string
}

interface Feed {
  id: string
  name: string
  smart_ranking?: boolean
  filters: Filter[]
}

interface Props {
  feed?: Feed
  onClose: () => void
}

const FILTER_TYPES = [
  { value: 'friend_group',      label: 'Include Friend Group' },
  { value: 'community_group',   label: 'Include Community Group' },
  { value: 'include_page',      label: 'Include Page' },
  { value: 'fediverse_account', label: 'Include Fediverse Account' },
  { value: 'fediverse_all',     label: 'Include All Fediverse Follows' },
  { value: 'exclude_friend',    label: 'Exclude Friend' },
  { value: 'exclude_group',     label: 'Exclude Community Group' },
  { value: 'exclude_page',      label: 'Exclude Page' },
]

// fediverse_all has no meaningful "value" to pick — it means "everyone I follow".
const NO_VALUE_FILTER_TYPES = new Set(['fediverse_all'])

export default function FeedBuilderModal({ feed, onClose }: Props) {
  const qc = useQueryClient()
  const isEditing = !!feed

  const [name, setName] = useState(feed?.name ?? '')
  const [smartRanking, setSmartRanking] = useState(feed?.smart_ranking ?? false)
  const [rules, setRules] = useState<Filter[]>(feed?.filters?.map(f => ({ filter_type: f.filter_type, value: f.value })) ?? [])
  const [error, setError] = useState('')
  const [resetDone, setResetDone] = useState(false)

  const { data: listsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listFriendLists().then(r => r.data),
  })
  const { data: friendsData } = useQuery({
    queryKey: ['friends'],
    queryFn: () => friendsApi.listFriends().then(r => r.data),
  })
  const { data: groupsData } = useQuery({
    queryKey: ['groups', 'all'],
    queryFn: () => groupsApi.list({ page: 0 }).then(r => r.data),
  })
  const { data: myPagesData } = useQuery({
    queryKey: ['pages-mine'],
    queryFn: () => pagesApi.mine().then(r => r.data),
    staleTime: 60_000,
  })
  const { data: followingData } = useQuery({
    queryKey: ['fediverse-following'],
    queryFn: () => federationApi.listFollowing().then(r => r.data),
  })

  const friendGroups = listsData?.groups ?? []
  const friends = friendsData?.friends ?? []
  const myGroups = (groupsData?.groups ?? []).filter((g: any) => g.is_member)
  const myPages: any[] = myPagesData?.pages ?? []
  const following: any[] = followingData?.following ?? []

  const save = useMutation({
    mutationFn: (data: { name: string, smart_ranking: boolean, filters: Filter[] }) =>
      isEditing ? customFeedsApi.update(feed!.id, data) : customFeedsApi.create(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['custom-feeds'] })
      onClose()
    },
  })

  const resetInteractions = useMutation({
    mutationFn: () => interactionsApi.reset(),
    onSuccess: () => { setResetDone(true); setTimeout(() => setResetDone(false), 3000) },
  })

  function addRule() {
    setRules(r => [...r, { filter_type: 'friend_group', value: '' }])
  }

  function removeRule(idx: number) {
    setRules(r => r.filter((_, i) => i !== idx))
  }

  function updateRule(idx: number, field: keyof Filter, val: string) {
    setRules(r => r.map((rule, i) => {
      if (i !== idx) return rule
      if (field === 'filter_type') return { filter_type: val, value: NO_VALUE_FILTER_TYPES.has(val) ? 'true' : '' }
      return { ...rule, [field]: val }
    }))
  }

  function getOptions(filterType: string) {
    switch (filterType) {
      case 'friend_group':
        return friendGroups.map((g: any) => ({ id: g.id, label: g.name }))
      case 'community_group':
      case 'exclude_group':
        return myGroups.map((g: any) => ({ id: g.id, label: g.name }))
      case 'exclude_friend':
        return friends.map((f: any) => ({ id: f.id, label: f.display_name || f.username }))
      case 'include_page':
      case 'exclude_page':
        return myPages.map((p: any) => ({ id: p.id, label: p.display_name }))
      case 'fediverse_account':
        // A followed account only gets a local stub (users.id) once its first
        // post has been ingested — nothing to filter by before that, so it's
        // left out of the picker until then.
        return following
          .filter((f: any) => f.user_id)
          .map((f: any) => ({ id: f.user_id, label: f.display_name || f.username || f.actor_url }))
      default:
        return []
    }
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (!name.trim()) { setError('Feed name is required.'); return }
    if (rules.length === 0) { setError('Add at least one filter rule.'); return }
    const incomplete = rules.find(r => !r.value)
    if (incomplete) { setError('All filter rules must have a value selected.'); return }
    save.mutate({ name: name.trim(), smart_ranking: smartRanking, filters: rules })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-white dark:bg-agora-900 rounded-xl shadow-xl w-full max-w-lg max-h-[90vh] flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-agora-100 dark:border-agora-700">
          <h2 className="font-bold text-agora-900 dark:text-agora-100">
            {isEditing ? 'Edit Feed' : 'New Custom Feed'}
          </h2>
          <button onClick={onClose} className="text-agora-400 hover:text-agora-700 dark:hover:text-agora-200">
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden">
          <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
            {/* Feed name */}
            <div>
              <label className="block text-sm font-medium text-agora-700 dark:text-agora-300 mb-1">
                Feed Name
              </label>
              <input
                type="text"
                value={name}
                onChange={e => setName(e.target.value)}
                placeholder="e.g. Close Friends, Work Updates"
                maxLength={100}
                className="w-full input"
              />
            </div>

            {/* Filter rules */}
            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-sm font-medium text-agora-700 dark:text-agora-300">
                  Filter Rules
                </label>
                <button type="button" onClick={addRule}
                  className="flex items-center gap-1 text-xs text-agora-600 dark:text-agora-400 hover:text-agora-800 dark:hover:text-agora-200 font-medium">
                  <Plus size={14} /> Add Rule
                </button>
              </div>

              {rules.length === 0 && (
                <p className="text-sm text-agora-400 italic py-3 text-center border border-dashed border-agora-200 dark:border-agora-700 rounded-lg">
                  No rules yet. Add at least one filter rule.
                </p>
              )}

              <div className="space-y-2">
                {rules.map((rule, idx) => {
                  const options = getOptions(rule.filter_type)
                  return (
                    <div key={idx} className="flex gap-2 items-start">
                      <div className="flex-1 flex gap-2">
                        <select
                          value={rule.filter_type}
                          onChange={e => updateRule(idx, 'filter_type', e.target.value)}
                          className="flex-1 input text-sm"
                        >
                          {FILTER_TYPES.map(ft => (
                            <option key={ft.value} value={ft.value}>{ft.label}</option>
                          ))}
                        </select>
                        {!NO_VALUE_FILTER_TYPES.has(rule.filter_type) && (
                          <select
                            value={rule.value}
                            onChange={e => updateRule(idx, 'value', e.target.value)}
                            className="flex-1 input text-sm"
                          >
                            <option value="">— Select —</option>
                            {options.map((opt: any) => (
                              <option key={opt.id} value={opt.id}>{opt.label}</option>
                            ))}
                          </select>
                        )}
                      </div>
                      <button type="button" onClick={() => removeRule(idx)}
                        className="mt-2 text-red-400 hover:text-red-600 flex-shrink-0">
                        <Trash2 size={15} />
                      </button>
                    </div>
                  )
                })}
              </div>
            </div>

            {/* Smart Ranking toggle (AGORA-103 / AGORA-104) */}
            <div className={`rounded-xl border p-4 space-y-2 transition-colors ${
              smartRanking
                ? 'border-agora-400 bg-agora-50 dark:bg-agora-800/50'
                : 'border-agora-200 dark:border-agora-600'
            }`}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Sparkles size={15} className={smartRanking ? 'text-agora-600' : 'text-agora-400'} />
                  <span className="text-sm font-medium text-agora-700 dark:text-agora-200">Smart Ranking</span>
                </div>
                <button
                  type="button"
                  onClick={() => setSmartRanking(v => !v)}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    smartRanking ? 'bg-agora-600' : 'bg-agora-200 dark:bg-agora-600'
                  }`}
                >
                  <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform ${
                    smartRanking ? 'translate-x-4' : 'translate-x-1'
                  }`} />
                </button>
              </div>
              {smartRanking && (
                <div className="space-y-2">
                  <p className="text-xs text-agora-500">
                    Posts from people you interact with most will appear higher. Ranking combines engagement history with recency so old posts don't resurface.
                  </p>
                  <button
                    type="button"
                    onClick={() => resetInteractions.mutate()}
                    disabled={resetInteractions.isPending || resetDone}
                    className="flex items-center gap-1.5 text-xs text-agora-500 hover:text-agora-700 transition-colors"
                  >
                    <RotateCcw size={11} />
                    {resetDone ? 'History cleared ✓' : resetInteractions.isPending ? 'Clearing…' : 'Reset my interaction history'}
                  </button>
                </div>
              )}
            </div>

            {error && <p className="text-sm text-red-500">{error}</p>}
            {save.isError && (
              <p className="text-sm text-red-500">
                {(save.error as any)?.response?.data?.error ?? 'Something went wrong.'}
              </p>
            )}
          </div>

          <div className="px-5 py-4 border-t border-agora-100 dark:border-agora-700 flex justify-end gap-2">
            <button type="button" onClick={onClose} className="btn-secondary text-sm">
              Cancel
            </button>
            <button type="submit" disabled={save.isPending} className="btn-primary text-sm">
              {save.isPending ? 'Saving…' : isEditing ? 'Save Changes' : 'Create Feed'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
