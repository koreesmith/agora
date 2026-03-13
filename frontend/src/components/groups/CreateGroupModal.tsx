import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { groupsApi } from '../../api'
import { X, Globe, Lock } from 'lucide-react'

export default function CreateGroupModal({ onClose, onCreated }: { onClose: () => void, onCreated: (slug: string) => void }) {
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [privacy, setPrivacy] = useState<'public'|'private'>('public')
  const [err, setErr] = useState('')

  const create = useMutation({
    mutationFn: () => groupsApi.create({ name, description, privacy }),
    onSuccess: (res) => {
      onCreated(res.data.slug)
      navigate(`/groups/${res.data.slug}`)
    },
    onError: (e: any) => setErr(e.response?.data?.error || 'Could not create group'),
  })

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-bold">Create a Group</h2>
          <button onClick={onClose} className="btn-ghost p-1"><X size={18} /></button>
        </div>

        {err && <p className="text-sm text-red-500 bg-red-50 dark:bg-red-900/20 rounded-lg px-3 py-2">{err}</p>}

        <div className="space-y-3">
          <div>
            <label className="label">Group name <span className="text-red-500">*</span></label>
            <input className="input" placeholder="e.g. Photography Enthusiasts" value={name} onChange={e => setName(e.target.value)} />
          </div>
          <div>
            <label className="label">Description</label>
            <textarea className="input resize-none" rows={3} placeholder="What is this group about?" value={description} onChange={e => setDescription(e.target.value)} />
          </div>
          <div>
            <label className="label">Privacy</label>
            <div className="grid grid-cols-2 gap-2 mt-1">
              {([['public', Globe, 'Public', 'Anyone can find and join'], ['private', Lock, 'Private', 'Members only — content is hidden from non-members']] as const).map(([val, Icon, label, desc]) => (
                <button key={val} onClick={() => setPrivacy(val)}
                  className={`flex flex-col items-start p-3 rounded-lg border-2 transition-colors text-left ${privacy === val ? 'border-agora-600 bg-agora-50 dark:bg-agora-700' : 'border-agora-200 dark:border-agora-600 hover:border-agora-300'}`}>
                  <div className="flex items-center gap-1.5 font-medium text-sm"><Icon size={14} />{label}</div>
                  <p className="text-xs text-agora-400 mt-0.5">{desc}</p>
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="flex gap-2 justify-end pt-1">
          <button onClick={onClose} className="btn-secondary">Cancel</button>
          <button onClick={() => create.mutate()} disabled={!name.trim() || create.isPending} className="btn-primary">
            {create.isPending ? 'Creating…' : 'Create Group'}
          </button>
        </div>
      </div>
    </div>
  )
}
