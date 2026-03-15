import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Image, X, Globe, Users, Lock, AlertTriangle, ExternalLink, BarChart2, Plus, Minus } from 'lucide-react'
import { feedApi, friendsApi, previewApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { useMentions } from './useMentions'
import MentionDropdown from './MentionDropdown'

// Detect the first URL in a string
const URL_RE = /https?:\/\/[^\s<>"{}|\\^`[\]]+/i

interface Preview {
  url: string
  title: string
  description: string
  image: string
  domain: string
}

export default function CreatePost() {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [content, setContent] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [visibility, setVisibility] = useState('friends')
  const [groupId, setGroupId] = useState('')
  const [uploading, setUploading] = useState(false)
  const [twEnabled, setTwEnabled] = useState(false)
  const [twLabel, setTwLabel] = useState('')
  const [pollEnabled, setPollEnabled] = useState(false)
  const [pollOptions, setPollOptions] = useState(['', ''])
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewDismissed, setPreviewDismissed] = useState(false)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [detectedUrl, setDetectedUrl] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastFetchedUrl = useRef('')

  const { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()

  const { data: groupsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
  })
  const groups = groupsData?.groups || []

  // Debounced URL detection — fires 800ms after user stops typing
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)

    const match = content.match(URL_RE)
    const url = match ? match[0].replace(/[.,!?)]+$/, '') : '' // strip trailing punctuation

    if (!url) {
      setPreview(null)
      setPreviewError('')
      setDetectedUrl('')
      setPreviewDismissed(false)
      lastFetchedUrl.current = ''
      return
    }

    // Don't re-fetch the same URL, and don't fetch if user dismissed this URL
    if (url === lastFetchedUrl.current) return
    if (previewDismissed && url === detectedUrl) return

    debounceRef.current = setTimeout(async () => {
      lastFetchedUrl.current = url
      setPreviewLoading(true)
      setPreviewError('')
      try {
        const res = await previewApi.fetch(url)
        if (res.data?.title || res.data?.description) {
          setPreview(res.data)
          setDetectedUrl(url)
        } else {
          setPreview(null)
        }
      } catch (err: any) {
        setPreview(null)
        const msg = err?.response?.data?.error
        if (msg) setPreviewError(msg)
      } finally {
        setPreviewLoading(false)
      }
    }, 800)
  }, [content, previewDismissed, detectedUrl])

  const dismissPreview = () => {
    setPreview(null)
    setPreviewError('')
    setPreviewDismissed(true)
  }

  const create = useMutation({
    mutationFn: () => feedApi.createPost({
      content,
      image_url: imageUrl,
      visibility,
      group_id: visibility === 'group' ? groupId : undefined,
      content_warning: twEnabled && twLabel.trim() ? twLabel.trim() : '',
      link_url: preview ? preview.url : '',
      link_title: preview ? preview.title : '',
      link_description: preview ? preview.description : '',
      link_image: preview ? preview.image : '',
      link_domain: preview ? preview.domain : '',
      poll_options: pollEnabled ? pollOptions.filter(o => o.trim()) : [],
    }),
    onSuccess: () => {
      setContent(''); setImageUrl(''); setGroupId('')
      setTwEnabled(false); setTwLabel('')
      setPollEnabled(false); setPollOptions(['', ''])
      setPreview(null); setDetectedUrl(''); setPreviewDismissed(false)
      qc.invalidateQueries({ queryKey: ['feed'] })
    },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch (err: any) {
      const msg = err?.response?.data?.error || 'Image upload failed. Please try a JPEG or PNG file.'
      alert(msg)
    }
    finally { setUploading(false) }
  }

  const visOptions = [
    { value: 'public',  icon: Globe,  label: 'Public' },
    { value: 'friends', icon: Users,  label: 'Friends' },
    { value: 'group',   icon: Lock,   label: 'Friend List' },
  ]

  const validPoll = !pollEnabled || pollOptions.filter(o => o.trim()).length >= 2
  const canPost = (content.trim() || imageUrl || (pollEnabled && pollOptions.filter(o => o.trim()).length >= 2))
    && !create.isPending && !uploading && (!twEnabled || twLabel.trim()) && validPoll

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
            autoComplete="off"
            className="w-full resize-none bg-transparent text-sm text-agora-800 dark:text-agora-200 placeholder-agora-400 focus:outline-none"
          />
          {showMentions && <MentionDropdown users={mentionUsers} onSelect={u => insertMention(content, setContent, u)} />}
        </div>
      </div>

      {/* Uploaded image preview */}
      {imageUrl && (
        <div className="relative ml-13">
          <img src={imageUrl} alt="" className="rounded-lg w-full max-h-48 object-contain bg-agora-50 dark:bg-agora-900" />
          <button onClick={() => setImageUrl('')} className="absolute top-2 right-2 bg-black/60 text-white rounded-full w-6 h-6 flex items-center justify-center hover:bg-black/80">
            <X size={12} />
          </button>
        </div>
      )}

      {/* Link preview loading */}
      {previewLoading && !imageUrl && (
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl p-3 flex items-center gap-2 text-xs text-agora-400">
          <div className="w-3 h-3 border-2 border-agora-400 border-t-transparent rounded-full animate-spin" />
          Fetching link preview…
        </div>
      )}

      {/* Link preview error (only shown when backend returns a specific message) */}
      {previewError && !previewLoading && !imageUrl && (
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl px-3 py-2 flex items-center justify-between text-xs text-agora-400">
          <span>Could not load preview: {previewError}</span>
          <button onClick={dismissPreview} className="text-agora-300 hover:text-agora-500"><X size={12} /></button>
        </div>
      )}

      {/* Link preview card */}
      {preview && !previewLoading && !imageUrl && (
        <div className="relative border border-agora-200 dark:border-agora-600 rounded-xl overflow-hidden group">
          <button
            onClick={dismissPreview}
            className="absolute top-2 right-2 z-10 bg-black/40 text-white rounded-full w-5 h-5 flex items-center justify-center hover:bg-black/70"
          >
            <X size={10} />
          </button>
          <a href={preview.url} target="_blank" rel="noreferrer" className="flex gap-3 p-3 hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors">
            {preview.image && (
              <img
                src={preview.image}
                alt=""
                className="w-20 h-20 object-cover rounded-lg flex-shrink-0 bg-agora-100 dark:bg-agora-700"
                onError={e => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
            )}
            <div className="flex-1 min-w-0 space-y-0.5">
              <p className="text-xs text-agora-400 flex items-center gap-1">
                <ExternalLink size={10} /> {preview.domain}
              </p>
              {preview.title && <p className="text-sm font-semibold line-clamp-2 text-agora-800 dark:text-agora-200">{preview.title}</p>}
              {preview.description && <p className="text-xs text-agora-500 line-clamp-2">{preview.description}</p>}
            </div>
          </a>
        </div>
      )}

      {/* Trigger warning label input */}
      {twEnabled && (
        <div className="flex items-center gap-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-700 rounded-lg px-3 py-2">
          <AlertTriangle size={14} className="text-amber-500 flex-shrink-0" />
          <input
            className="flex-1 bg-transparent text-sm text-amber-800 dark:text-amber-200 placeholder-amber-400 focus:outline-none"
            placeholder="Describe the trigger (e.g. violence, spiders, grief)…"
            autoComplete="off"
            value={twLabel}
            onChange={e => setTwLabel(e.target.value)}
            autoFocus
            maxLength={120}
          />
        </div>
      )}

      {/* Poll editor */}
      {pollEnabled && (
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl p-3 space-y-2">
          <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">Poll options</p>
          {pollOptions.map((opt, i) => (
            <div key={i} className="flex items-center gap-2">
              <input
                className="input flex-1 text-sm"
                autoComplete="off"
                placeholder={i < 2 ? `Option ${i + 1} (required)` : `Option ${i + 1} (optional)`}
                value={opt}
                maxLength={100}
                onChange={e => setPollOptions(opts => opts.map((o, j) => j === i ? e.target.value : o))}
              />
              {pollOptions.length > 2 && (
                <button
                  onClick={() => setPollOptions(opts => opts.filter((_, j) => j !== i))}
                  className="text-agora-400 hover:text-red-500 transition-colors flex-shrink-0"
                >
                  <Minus size={14} />
                </button>
              )}
            </div>
          ))}
          {pollOptions.length < 6 && (
            <button
              onClick={() => setPollOptions(opts => [...opts, ''])}
              className="flex items-center gap-1.5 text-xs text-agora-500 hover:text-agora-700 transition-colors"
            >
              <Plus size={12} /> Add option
            </button>
          )}
        </div>
      )}

      <div className="flex items-center gap-2 pt-2 border-t border-agora-100 dark:border-agora-700">
        {/* Image upload */}
        <label className="btn-ghost p-2 cursor-pointer" title="Add image">
          <Image size={18} />
          <input type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading || !!imageUrl} />
        </label>

        {/* Trigger warning toggle */}
        <button
          onClick={() => { setTwEnabled(v => !v); if (twEnabled) setTwLabel('') }}
          title="Trigger warning"
          className={`flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs font-medium border transition-colors ${
            twEnabled
              ? 'bg-amber-100 dark:bg-amber-900/30 border-amber-400 text-amber-700 dark:text-amber-400'
              : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-amber-400 hover:text-amber-500'
          }`}
        >
          <AlertTriangle size={13} /> TW
        </button>

        {/* Poll toggle */}
        <button
          onClick={() => { setPollEnabled(v => !v); if (pollEnabled) setPollOptions(['', '']) }}
          title="Add poll"
          className={`flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs font-medium border transition-colors ${
            pollEnabled
              ? 'bg-agora-100 dark:bg-agora-700 border-agora-400 text-agora-700 dark:text-agora-200'
              : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-agora-400 hover:text-agora-600'
          }`}
        >
          <BarChart2 size={13} /> Poll
        </button>

        {/* Visibility */}
        <select value={visibility} onChange={e => setVisibility(e.target.value)}
          className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none">
          {visOptions.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>

        {/* Friend list picker */}
        {visibility === 'group' && (
          groups.length > 0
            ? <select value={groupId} onChange={e => setGroupId(e.target.value)}
                className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none">
                <option value="">Select list…</option>
                {groups.map((g: any) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            : <span className="text-xs text-agora-400">No lists yet — <Link to="/friends" className="underline">create one</Link></span>
        )}

        <button onClick={() => create.mutate()} disabled={!canPost} className="ml-auto btn-primary text-sm">
          {create.isPending ? 'Posting…' : 'Post'}
        </button>
      </div>
    </div>
  )
}
