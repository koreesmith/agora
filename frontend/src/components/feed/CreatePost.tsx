import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Image, X, Globe, Users, Lock, AlertTriangle, ExternalLink, BarChart2, Plus, Minus, ChevronDown, Video } from 'lucide-react'
import { feedApi, friendsApi, previewApi, pagesApi } from '../../api'
import api from '../../api'
import { useAuthStore } from '../../store/auth'
import { useMentions } from './useMentions'
import MentionDropdown from './MentionDropdown'
import { isGifUrl } from '../../utils/gif'

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
  const [imageUrls, setImageUrls] = useState<string[]>([])
  const [visibility, setVisibility] = useState('friends')
  // Post-as-page identity selector
  const [selectedPageSlug, setSelectedPageSlug] = useState<string>('')
  const [showPagePicker, setShowPagePicker] = useState(false)
  // AGORA-119: video state
  const [videoUrl, setVideoUrl] = useState('')
  const [videoThumbUrl, setVideoThumbUrl] = useState('')
  const [uploadingVideo, setUploadingVideo] = useState(false)
  // AGORA-89: group tag autocomplete
  const [groupSuggestions, setGroupSuggestions] = useState<any[]>([])
  const [showGroupSuggestions, setShowGroupSuggestions] = useState(false)
  const [groupTagQuery, setGroupTagQuery] = useState('')
  const groupDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [friendListId, setFriendListId] = useState('')
  const [uploading, setUploading] = useState(false)
  const [showUploadModal, setShowUploadModal] = useState(false)
  const uploadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [twEnabled, setTwEnabled] = useState(false)
  const [twLabel, setTwLabel] = useState('')
  const [pollEnabled, setPollEnabled] = useState(false)
  const [pollOptions, setPollOptions] = useState(['', ''])
  const [pollMultipleChoice, setPollMultipleChoice] = useState(false)
  const [pollAllowsNewOptions, setPollAllowsNewOptions] = useState(false)
  const [pollExpiresHours, setPollExpiresHours] = useState(24)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewDismissed, setPreviewDismissed] = useState(false)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [detectedUrl, setDetectedUrl] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastFetchedUrl = useRef('')

  const { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()

  const { data: groupsData } = useQuery({
    queryKey: ['friend-lists'],
    queryFn: () => friendsApi.listFriendLists().then(r => r.data),
  })
  const friendLists = groupsData?.groups || []

  // Load pages owned by current user (for post-as-page)
  const { data: myPagesData } = useQuery({
    queryKey: ['pages-mine'],
    queryFn: () => pagesApi.mine().then(r => r.data),
    staleTime: 60_000,
  })
  const myPages: any[] = myPagesData?.pages ?? []
  const selectedPage = myPages.find(p => p.slug === selectedPageSlug)

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

    // GIF URLs: set as image directly, skip link preview entirely
    if (isGifUrl(url)) {
      if (imageUrls.length === 0) {
        setImageUrls([url])
        // Strip the URL from the content so it doesn't show as text
        setContent(c => c.replace(url, '').trim())
      }
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

  // AGORA-117: show upload modal after 1s to reassure users during slow HEIC conversion
  useEffect(() => {
    if (uploading) {
      uploadTimerRef.current = setTimeout(() => setShowUploadModal(true), 1000)
    } else {
      if (uploadTimerRef.current) clearTimeout(uploadTimerRef.current)
      setShowUploadModal(false)
    }
    return () => { if (uploadTimerRef.current) clearTimeout(uploadTimerRef.current) }
  }, [uploading])

  const dismissPreview = () => {
    setPreview(null)
    setPreviewError('')
    setPreviewDismissed(true)
  }

  const create = useMutation({
    mutationFn: () => {
      // If a page is selected, post as that page instead
      if (selectedPageSlug) {
        return pagesApi.createPost(selectedPageSlug, {
          content,
          image_urls: imageUrls,
        })
      }
      return feedApi.createPost({
        content,
        image_urls: imageUrls,
        video_url: videoUrl || undefined,
        video_thumb_url: videoThumbUrl || undefined,
        visibility,
        group_id: visibility === 'group' ? friendListId : undefined,
        content_warning: twEnabled && twLabel.trim() ? twLabel.trim() : '',
        link_url: preview ? preview.url : '',
        link_title: preview ? preview.title : '',
        link_description: preview ? preview.description : '',
        link_image: preview ? preview.image : '',
        link_domain: preview ? preview.domain : '',
        poll_options: pollEnabled ? pollOptions.filter(o => o.trim()) : [],
        poll_multiple_choice: pollEnabled ? pollMultipleChoice : false,
        poll_allows_new_options: pollEnabled ? pollAllowsNewOptions : false,
        poll_expires_hours: pollEnabled ? pollExpiresHours : 0,
      })
    },
    onSuccess: () => {
      setContent(''); setImageUrls([]); setFriendListId(''); setVideoUrl(''); setVideoThumbUrl('')
      setTwEnabled(false); setTwLabel('')
      setPollEnabled(false); setPollOptions(['', ''])
      setPollMultipleChoice(false); setPollAllowsNewOptions(false); setPollExpiresHours(24)
      setPreview(null); setDetectedUrl(''); setPreviewDismissed(false)
      qc.invalidateQueries({ queryKey: ['feed'] })
    },
  })

  // Shared image upload helper — accepts File[] from file input or clipboard
  const uploadFiles = async (files: File[]) => {
    if (!files.length) return
    const MAX_PHOTOS = 10
    const remaining = MAX_PHOTOS - imageUrls.length
    const toUpload = files.filter(f => f.type.startsWith('image/')).slice(0, remaining)
    if (!toUpload.length) return
    setUploading(true)
    try {
      const results = await Promise.all(toUpload.map(f => feedApi.uploadMedia(f, 'posts')))
      setImageUrls(prev => [...prev, ...results.map((r: any) => r.data.url)])
    } catch (err: any) {
      const msg = err?.response?.data?.error || 'Image upload failed. Please try a JPEG or PNG file.'
      alert(msg)
    } finally {
      setUploading(false)
    }
  }

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    await uploadFiles(Array.from(e.target.files || []))
    e.target.value = ''
  }

  // AGORA-76: paste images from clipboard directly into the compose box
  const handlePaste = async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const imageFiles = Array.from(e.clipboardData.items)
      .filter(item => item.type.startsWith('image/'))
      .map(item => item.getAsFile())
      .filter(Boolean) as File[]
    if (imageFiles.length > 0) {
      e.preventDefault()
      await uploadFiles(imageFiles)
    }
    // Non-image pastes (text, links) fall through to default behaviour
  }

  // AGORA-119: video upload
  const handleVideoUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploadingVideo(true)
    try {
      const res = await feedApi.uploadMedia(file, 'videos')
      setVideoUrl((res as any).data.url)
      setVideoThumbUrl((res as any).data.thumb_url || '')
      setImageUrls([]) // video and photos are mutually exclusive
    } catch (err: any) {
      alert(err?.response?.data?.error || 'Video upload failed. Make sure it is under 2 minutes and 200 MB.')
    } finally {
      setUploadingVideo(false)
      e.target.value = ''
    }
  }

  const visOptions = [
    { value: 'public',  icon: Globe,  label: 'Public' },
    { value: 'friends', icon: Users,  label: 'Friends' },
    { value: 'group',   icon: Lock,   label: 'Friend List' },
  ]

  const validPoll = !pollEnabled || pollOptions.filter(o => o.trim()).length >= 2
  const canPost = (content.trim() || imageUrls.length > 0 || videoUrl || (pollEnabled && pollOptions.filter(o => o.trim()).length >= 2))
    && !create.isPending && !uploading && (!twEnabled || twLabel.trim()) && validPoll

  return (
    <>
    {/* AGORA-117: upload progress modal — shown after 1s of uploading */}
    {showUploadModal && (
      <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
        <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-xs p-6 text-center space-y-3">
          <div className="w-10 h-10 border-4 border-agora-600 border-t-transparent rounded-full animate-spin mx-auto" />
          <p className="font-semibold text-agora-800 dark:text-agora-100">Uploading photo…</p>
          <p className="text-sm text-agora-500">
            HEIC photos from your camera roll are being converted to JPEG. This may take a moment — please wait.
          </p>
        </div>
      </div>
    )}
    <div className="card p-4 space-y-3">
      <div className="flex gap-3">
        {/* Avatar — shows selected page avatar when posting as a page */}
        <div className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {selectedPage?.avatar_url
            ? <img src={selectedPage.avatar_url} alt="" className="w-full h-full object-cover" />
            : selectedPage
              ? <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 dark:text-agora-300 rounded-full">{selectedPage.display_name[0]}</span>
              : user?.avatar_url
              ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 dark:text-agora-300">{user?.username?.[0]?.toUpperCase()}</span>}
        </div>
        <div className="flex-1 relative">
          {/* Identity picker — only shown when user owns ≥1 page */}
          {myPages.length > 0 && (
            <div className="relative mb-2">
              <button
                type="button"
                onClick={() => setShowPagePicker(v => !v)}
                className="flex items-center gap-1.5 text-xs font-medium text-agora-600 dark:text-agora-300 bg-agora-50 dark:bg-agora-700/50 border border-agora-200 dark:border-agora-600 rounded-lg px-2.5 py-1 hover:border-agora-400 transition-colors">
                {selectedPage ? (
                  <><span className="truncate max-w-32">{selectedPage.display_name}</span> <ChevronDown size={11} /></>
                ) : (
                  <><span>{user?.username}</span> <ChevronDown size={11} /></>
                )}
              </button>
              {showPagePicker && (
                <div className="absolute left-0 top-full mt-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg py-1 min-w-44 z-30"
                  onMouseLeave={() => setShowPagePicker(false)}>
                  <button
                    onClick={() => { setSelectedPageSlug(''); setShowPagePicker(false) }}
                    className={`w-full text-left px-3 py-2 text-sm hover:bg-agora-50 dark:hover:bg-agora-700 flex items-center gap-2 ${!selectedPageSlug ? 'font-semibold' : ''}`}>
                    <div className="w-5 h-5 rounded-full bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0">
                      {user?.avatar_url
                        ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
                        : <span className="w-full h-full flex items-center justify-center text-xs font-bold">{user?.username?.[0]?.toUpperCase()}</span>}
                    </div>
                    <span className="truncate">{user?.username}</span>
                  </button>
                  <div className="border-t border-agora-100 dark:border-agora-700 my-1" />
                  {myPages.map((p: any) => (
                    <button key={p.slug}
                      onClick={() => { setSelectedPageSlug(p.slug); setShowPagePicker(false) }}
                      className={`w-full text-left px-3 py-2 text-sm hover:bg-agora-50 dark:hover:bg-agora-700 flex items-center gap-2 ${selectedPageSlug === p.slug ? 'font-semibold' : ''}`}>
                      <div className="w-5 h-5 rounded-lg bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0">
                        {p.avatar_url
                          ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
                          : <span className="w-full h-full flex items-center justify-center text-xs font-bold">{p.display_name[0]}</span>}
                      </div>
                      <span className="truncate">{p.display_name}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <textarea
            ref={inputRef as React.RefObject<HTMLTextAreaElement>}
            value={content}
            onChange={e => {
              const val = e.target.value
              const pos = e.target.selectionStart ?? val.length
              setContent(val)
              handleChange(val, pos)
              // AGORA-89: group tag autocomplete — detect +word before cursor
              const before = val.slice(0, pos)
              const groupMatch = before.match(/\+([a-zA-Z0-9_-]*)$/)
              if (groupMatch) {
                const q = groupMatch[1]
                setGroupTagQuery(q)
                if (groupDebounceRef.current) clearTimeout(groupDebounceRef.current)
                groupDebounceRef.current = setTimeout(async () => {
                  const res = await api.get('/groups/mention-search', { params: { q } })
                  setGroupSuggestions(res.data.groups || [])
                  setShowGroupSuggestions(true)
                }, 200)
              } else {
                setShowGroupSuggestions(false)
                setGroupSuggestions([])
              }
            }}
            onKeyDown={e => { if (e.key === 'Escape') dismiss() }}
            onPaste={handlePaste}
            placeholder="What's on your mind? Use @username to tag someone."
            rows={3}
            autoComplete="off"
            data-1p-ignore="true"
            data-lpignore="true"
            data-form-type="other"
            className="w-full resize-none bg-transparent text-sm text-agora-800 dark:text-agora-200 placeholder-agora-400 focus:outline-none"
          />
          {showMentions && <MentionDropdown users={mentionUsers} onSelect={u => insertMention(content, setContent, u)} />}
          {/* AGORA-89: group tag suggestions */}
          {showGroupSuggestions && groupSuggestions.length > 0 && (
            <div className="absolute left-0 top-full mt-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg py-1 z-40 w-56">
              {groupSuggestions.map((g: any) => (
                <button
                  key={g.slug}
                  type="button"
                  className="w-full text-left px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 flex items-center gap-2"
                  onMouseDown={e => {
                    e.preventDefault()
                    // Replace the +partial with +slug
                    const newContent = content.replace(/\+([a-zA-Z0-9_-]*)$/, `+${g.slug} `)
                    setContent(newContent)
                    setShowGroupSuggestions(false)
                    setGroupSuggestions([])
                  }}
                >
                  <div className="w-6 h-6 rounded-lg bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0">
                    {g.avatar_url
                      ? <img src={g.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-500">{g.name[0]}</span>}
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{g.name}</p>
                    <p className="text-xs text-agora-400">+{g.slug}</p>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Video preview (AGORA-119) */}
      {videoUrl && (
        <div className="ml-13 relative rounded-xl overflow-hidden bg-black">
          <video src={videoUrl} poster={videoThumbUrl || undefined} controls className="w-full max-h-72 rounded-xl" />
          <button onClick={() => { setVideoUrl(''); setVideoThumbUrl('') }}
            className="absolute top-2 right-2 bg-black/60 text-white rounded-full w-6 h-6 flex items-center justify-center hover:bg-black/80">
            <X size={12} />
          </button>
        </div>
      )}

      {/* Uploaded images preview */}
      {imageUrls.length > 0 && (
        <div className={`ml-13 grid gap-1.5 ${imageUrls.length === 1 ? 'grid-cols-1' : imageUrls.length === 2 ? 'grid-cols-2' : imageUrls.length === 3 ? 'grid-cols-2' : 'grid-cols-2'}`}>
          {imageUrls.map((url, idx) => (
            <div key={idx} className={`relative ${imageUrls.length === 3 && idx === 2 ? 'col-span-2' : ''}`}>
              {isGifUrl(url) && (
                <span className="absolute top-1 left-1 bg-black/60 text-white text-xs font-bold px-1 py-0.5 rounded z-10">GIF</span>
              )}
              <img src={url} alt="" className={`rounded-lg w-full object-cover ${imageUrls.length === 1 ? 'max-h-48' : 'h-32'} ${isGifUrl(url) ? '' : 'bg-agora-50 dark:bg-agora-900'}`} />
              <button onClick={() => setImageUrls(prev => prev.filter((_, i) => i !== idx))} className="absolute top-1 right-1 bg-black/60 text-white rounded-full w-5 h-5 flex items-center justify-center hover:bg-black/80">
                <X size={10} />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Link preview loading */}
      {previewLoading && imageUrls.length === 0 && (
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl p-3 flex items-center gap-2 text-xs text-agora-400">
          <div className="w-3 h-3 border-2 border-agora-400 border-t-transparent rounded-full animate-spin" />
          Fetching link preview…
        </div>
      )}

      {/* Link preview error (only shown when backend returns a specific message) */}
      {previewError && !previewLoading && imageUrls.length === 0 && (
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl px-3 py-2 flex items-center justify-between text-xs text-agora-400">
          <span>Could not load preview: {previewError}</span>
          <button onClick={dismissPreview} className="text-agora-300 hover:text-agora-500"><X size={12} /></button>
        </div>
      )}

      {/* Link preview card */}
      {preview && !previewLoading && imageUrls.length === 0 && (
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
        <div className="border border-agora-200 dark:border-agora-600 rounded-xl p-3 space-y-3">
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

          {/* Poll settings */}
          <div className="border-t border-agora-100 dark:border-agora-700 pt-3 space-y-2">
            <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">Poll settings</p>
            <div className="flex items-center gap-3 flex-wrap">
              {/* Duration */}
              <div className="flex items-center gap-2">
                <label className="text-xs text-agora-500">Duration</label>
                <select
                  className="text-xs border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1 bg-white dark:bg-agora-800 text-agora-700 dark:text-agora-200"
                  value={pollExpiresHours}
                  onChange={e => setPollExpiresHours(Number(e.target.value))}
                >
                  <option value={1}>1 hour</option>
                  <option value={6}>6 hours</option>
                  <option value={12}>12 hours</option>
                  <option value={24}>1 day</option>
                  <option value={72}>3 days</option>
                  <option value={168}>1 week</option>
                  <option value={0}>No limit</option>
                </select>
              </div>
              {/* Multiple choice */}
              <label className="flex items-center gap-1.5 text-xs text-agora-500 cursor-pointer">
                <input
                  type="checkbox"
                  checked={pollMultipleChoice}
                  onChange={e => setPollMultipleChoice(e.target.checked)}
                  className="rounded"
                />
                Allow multiple selections
              </label>
              {/* Allow new options */}
              <label className="flex items-center gap-1.5 text-xs text-agora-500 cursor-pointer">
                <input
                  type="checkbox"
                  checked={pollAllowsNewOptions}
                  onChange={e => setPollAllowsNewOptions(e.target.checked)}
                  className="rounded"
                />
                Let respondents add options
              </label>
            </div>
          </div>
        </div>
      )}

      <div className="flex items-center gap-2 pt-2 border-t border-agora-100 dark:border-agora-700">
        {/* Image upload */}
        {/* Image upload */}
        <label className={`btn-ghost p-2 cursor-pointer flex items-center gap-1 ${(imageUrls.length >= 10 || !!videoUrl) ? 'opacity-40 pointer-events-none' : ''}`} title={videoUrl ? 'Remove video to add photos' : imageUrls.length >= 10 ? 'Maximum 10 photos' : 'Add photos'}>
          <Image size={18} />
          {imageUrls.length > 0 && <span className="text-xs font-medium text-agora-500">{imageUrls.length}/10</span>}
          <input type="file" accept="image/*" multiple className="hidden" onChange={handleImageUpload} disabled={uploading || imageUrls.length >= 10 || !!videoUrl} />
        </label>

        {/* Video upload (AGORA-119) */}
        <label className={`btn-ghost p-2 cursor-pointer flex items-center gap-1 ${(imageUrls.length > 0 || !!videoUrl || uploadingVideo) ? 'opacity-40 pointer-events-none' : ''}`} title={imageUrls.length > 0 ? 'Remove photos to add a video' : videoUrl ? 'Video already attached' : 'Add video (max 2 min)'}>
          {uploadingVideo
            ? <span className="text-xs text-agora-400 animate-pulse">Processing…</span>
            : <Video size={18} />}
          <input type="file" accept="video/*" className="hidden" onChange={handleVideoUpload} disabled={imageUrls.length > 0 || !!videoUrl || uploadingVideo} />
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
          friendLists.length > 0
            ? <select value={friendListId} onChange={e => setFriendListId(e.target.value)}
                className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none">
                <option value="">Select list…</option>
                {friendLists.map((g: any) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            : <span className="text-xs text-agora-400">No lists yet — <Link to="/friends" className="underline">create one</Link></span>
        )}

        <button onClick={() => create.mutate()} disabled={!canPost} className="ml-auto btn-primary text-sm">
          {create.isPending ? 'Posting…' : 'Post'}
        </button>
      </div>
    </div>
    </>
  )
}
