import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Image, X, Globe, Users, Lock } from 'lucide-react'
import { feedApi, friendsApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { useMentions } from './useMentions'
import MentionDropdown from './MentionDropdown'

export default function CreatePost() {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [content, setContent] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [visibility, setVisibility] = useState('friends')
  const [groupId, setGroupId] = useState('')
  const [uploading, setUploading] = useState(false)

  const { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()

  const { data: groupsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
  })
  const groups = groupsData?.groups || []

  const create = useMutation({
    mutationFn: () => feedApi.createPost({ content, image_url: imageUrl, visibility, group_id: visibility === 'group' ? groupId : undefined }),
    onSuccess: () => { setContent(''); setImageUrl(''); setGroupId(''); qc.invalidateQueries({ queryKey: ['feed'] }) },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch { alert('Image upload failed') }
    finally { setUploading(false) }
  }

  const visOptions = [
    { value: 'public',  icon: Globe,  label: 'Public' },
    { value: 'friends', icon: Users,  label: 'Friends' },
    { value: 'group',   icon: Lock,   label: 'Group' },
  ]

  return (
    <div className="card p-4 space-y-3">
      <div className="flex gap-3">
        <div className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {user?.avatar_url
            ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 dark:text-agora-300">{user?.username?.[0]?.toUpperCase()}</span>}
        </div>
        <div className="flex-1 relative">
          <textarea
            ref={inputRef as React.RefObject<HTMLTextAreaElement>}
            value={content}
            onChange={e => { setContent(e.target.value); handleChange(e.target.value, e.target.selectionStart ?? e.target.value.length) }}
            onKeyDown={e => { if (e.key === 'Escape') dismiss() }}
            placeholder="What's on your mind? Use @username to tag someone."
            rows={3}
            className="w-full resize-none bg-transparent text-sm text-agora-800 dark:text-agora-200 placeholder-agora-400 focus:outline-none"
          />
          {showMentions && <MentionDropdown users={mentionUsers} onSelect={u => insertMention(content, setContent, u)} />}
        </div>
      </div>

      {imageUrl && (
        <div className="relative ml-13">
          <img src={imageUrl} alt="" className="rounded-lg w-full max-h-48 object-cover" />
          <button onClick={() => setImageUrl('')} className="absolute top-2 right-2 bg-black/60 text-white rounded-full w-6 h-6 flex items-center justify-center hover:bg-black/80">
            <X size={12} />
          </button>
        </div>
      )}

      <div className="flex items-center gap-2 pt-2 border-t border-agora-100 dark:border-agora-700">
        <label className="btn-ghost p-2 cursor-pointer" title="Add image">
          <Image size={18} />
          <input type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading || !!imageUrl} />
        </label>
        <select value={visibility} onChange={e => setVisibility(e.target.value)}
          className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none">
          {visOptions.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        {visibility === 'group' && groups.length > 0 && (
          <select value={groupId} onChange={e => setGroupId(e.target.value)}
            className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none">
            <option value="">Select group…</option>
            {groups.map((g: any) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        )}
        <button onClick={() => create.mutate()} disabled={(!content.trim() && !imageUrl) || create.isPending || uploading} className="ml-auto btn-primary text-sm">
          {create.isPending ? 'Posting…' : 'Post'}
        </button>
      </div>
    </div>
  )
}
