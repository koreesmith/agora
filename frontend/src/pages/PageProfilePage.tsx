import { useState } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { pagesApi, feedApi, moderationApi } from '../api'
import { useAuthStore } from '../store/auth'
import {
  Users, Settings, Flag, X, Heart, MessageCircle, Image,
  CheckCircle, BookOpen, MoreHorizontal, PenLine, Pencil, Trash2,
} from 'lucide-react'

const PAGE_TYPE_LABELS: Record<string, string> = {
  band: 'Band',
  business: 'Business',
  organization: 'Organization',
  creator: 'Creator',
}

export default function PageProfilePage() {
  const { slug } = useParams<{ slug: string }>()!
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const navigate = useNavigate()

  const [showCompose, setShowCompose] = useState(false)
  const [composeContent, setComposeContent] = useState('')
  const [composeUploading, setComposeUploading] = useState(false)
  const [composeImageUrls, setComposeImageUrls] = useState<string[]>([])
  const [showReport, setShowReport] = useState(false)
  const [reportReason, setReportReason] = useState('')
  const [reportSent, setReportSent] = useState(false)
  const [showMenu, setShowMenu] = useState(false)
  const [feedPage, setFeedPage] = useState(0)
  const [openPostMenuId, setOpenPostMenuId] = useState<string | null>(null)
  const [editingPostId, setEditingPostId] = useState<string | null>(null)
  const [editContent, setEditContent] = useState('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['page', slug],
    queryFn: () => pagesApi.get(slug!).then(r => r.data),
  })
  const page = data?.page

  const { data: feedData, isLoading: feedLoading } = useQuery({
    queryKey: ['page-feed', slug, feedPage],
    queryFn: () => pagesApi.getFeed(slug!, feedPage).then(r => r.data),
    enabled: !!page,
  })
  const posts: any[] = feedData?.posts ?? []

  const subscribe = useMutation({
    mutationFn: () => pagesApi.subscribe(slug!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['page', slug] }),
  })
  const unsubscribe = useMutation({
    mutationFn: () => pagesApi.unsubscribe(slug!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['page', slug] }),
  })
  const deletePage = useMutation({
    mutationFn: () => pagesApi.delete(slug!),
    onSuccess: () => navigate('/pages'),
  })
  const likePost = useMutation({
    mutationFn: (id: string) => feedApi.likePost(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['page-feed', slug] }),
  })
  const unlikePost = useMutation({
    mutationFn: (id: string) => feedApi.unlikePost(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['page-feed', slug] }),
  })
  const editPost = useMutation({
    mutationFn: (id: string) => feedApi.editPost(id, { content: editContent }),
    onSuccess: () => {
      setEditingPostId(null)
      qc.invalidateQueries({ queryKey: ['page-feed', slug] })
    },
  })
  const deletePost = useMutation({
    mutationFn: (id: string) => feedApi.deletePost(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['page-feed', slug] })
      qc.invalidateQueries({ queryKey: ['page', slug] })
    },
  })
  const submitReport = useMutation({
    mutationFn: () => moderationApi.createReport({
      reported_page_id: page?.id,
      reason: reportReason,
    }),
    onSuccess: () => { setReportSent(true) },
  })

  const createPagePost = useMutation({
    mutationFn: () => pagesApi.createPost(slug!, {
      content: composeContent,
      image_urls: composeImageUrls,
    }),
    onSuccess: () => {
      setComposeContent('')
      setComposeImageUrls([])
      setShowCompose(false)
      qc.invalidateQueries({ queryKey: ['page-feed', slug] })
      qc.invalidateQueries({ queryKey: ['page', slug] })
    },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || [])
    if (!files.length) return
    const remaining = 4 - composeImageUrls.length
    setComposeUploading(true)
    try {
      const results = await Promise.all(
        files.slice(0, remaining).map(f => feedApi.uploadMedia(f, 'posts'))
      )
      setComposeImageUrls(prev => [...prev, ...results.map((r: any) => r.data.url)])
    } finally {
      setComposeUploading(false)
      e.target.value = ''
    }
  }

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (error || !page) return (
    <div className="card p-8 text-center space-y-2">
      <p className="font-semibold">Page not found</p>
      <Link to="/pages" className="text-sm text-agora-500 hover:underline">← Discover pages</Link>
    </div>
  )

  const isOwner = page.is_owner
  const canPost = composeContent.trim() || composeImageUrls.length > 0

  return (
    <>
      {/* Report modal */}
      {showReport && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4"
          onClick={() => { setShowReport(false); setReportSent(false); setReportReason('') }}>
          <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4"
            onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold">Report Page</h2>
              <button onClick={() => setShowReport(false)} className="btn-ghost p-1"><X size={18} /></button>
            </div>
            {reportSent ? (
              <div className="text-center py-4 space-y-2">
                <CheckCircle size={32} className="mx-auto text-green-500" />
                <p className="font-medium">Report submitted</p>
                <p className="text-sm text-agora-500">Thanks — our team will review this page.</p>
                <button onClick={() => { setShowReport(false); setReportSent(false) }} className="btn-secondary text-sm">Close</button>
              </div>
            ) : (
              <>
                <p className="text-sm text-agora-500">Why are you reporting this page?</p>
                <div className="space-y-2">
                  {['Spam', 'Harassment', 'Misinformation', 'Inappropriate content', 'Other'].map(r => (
                    <label key={r} className="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="report_reason" value={r} checked={reportReason === r}
                        onChange={() => setReportReason(r)} className="accent-agora-600" />
                      <span className="text-sm">{r}</span>
                    </label>
                  ))}
                </div>
                <div className="flex gap-2 justify-end">
                  <button onClick={() => setShowReport(false)} className="btn-secondary">Cancel</button>
                  <button onClick={() => submitReport.mutate()} disabled={!reportReason || submitReport.isPending}
                    className="btn-primary text-sm">
                    {submitReport.isPending ? 'Sending…' : 'Submit report'}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      <div className="space-y-4">
        {/* Header card */}
        <div className="card overflow-hidden">
          {/* Cover */}
          {page.cover_url
            ? <img src={page.cover_url} alt="" className="w-full h-36 object-cover"
                style={{ objectPosition: page.cover_position || '50% 50%' }} />
            : <div className="w-full h-20 bg-gradient-to-r from-agora-600 to-agora-400" />}

          <div className="p-4">
            <div className="flex gap-3 items-start">
              {/* Avatar */}
              <div className="w-16 h-16 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0 -mt-10 ring-2 ring-white dark:ring-agora-800">
                {page.avatar_url
                  ? <img src={page.avatar_url} alt="" className="w-full h-full object-cover" />
                  : <span className="w-full h-full flex items-center justify-center text-2xl font-bold text-agora-500">
                      {page.display_name[0]}
                    </span>}
              </div>

              {/* Name + meta */}
              <div className="flex-1 min-w-0 pt-1">
                <div className="flex items-center gap-2 flex-wrap">
                  <h1 className="text-lg font-bold truncate">{page.display_name}</h1>
                  {page.is_verified && (
                    <span className="text-xs bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 px-1.5 py-0.5 rounded font-medium">✓ Verified</span>
                  )}
                  {page.page_type && (
                    <span className="text-xs bg-agora-100 dark:bg-agora-700 text-agora-600 dark:text-agora-300 px-1.5 py-0.5 rounded">
                      {PAGE_TYPE_LABELS[page.page_type] ?? page.page_type}
                    </span>
                  )}
                </div>
                <p className="text-xs text-agora-400 mt-0.5">@{page.slug}</p>
                <div className="flex items-center gap-3 mt-1 text-xs text-agora-500">
                  <span className="flex items-center gap-1"><Users size={11} /> {page.subscriber_count} subscribers</span>
                  <span className="flex items-center gap-1"><BookOpen size={11} /> {page.post_count} posts</span>
                </div>
              </div>

              {/* Actions */}
              <div className="flex items-center gap-2 flex-shrink-0">
                {isOwner ? (
                  <>
                    <button onClick={() => setShowCompose(v => !v)}
                      className="btn-primary text-sm flex items-center gap-1.5">
                      <PenLine size={14} /> New Post
                    </button>
                    <Link to={`/pages/${slug}/settings`}
                      className="btn-secondary text-sm flex items-center gap-1.5">
                      <Settings size={14} /> Edit
                    </Link>
                  </>
                ) : (
                  page.is_subscribed
                    ? <button onClick={() => unsubscribe.mutate()} disabled={unsubscribe.isPending}
                        className="btn-secondary text-sm">Subscribed</button>
                    : <button onClick={() => subscribe.mutate()} disabled={subscribe.isPending}
                        className="btn-primary text-sm">Subscribe</button>
                )}

                {/* Overflow menu */}
                <div className="relative">
                  <button onClick={() => setShowMenu(v => !v)} className="btn-ghost p-2">
                    <MoreHorizontal size={16} />
                  </button>
                  {showMenu && (
                    <div className="absolute right-0 top-full mt-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg py-1 min-w-36 z-20"
                      onMouseLeave={() => setShowMenu(false)}>
                      {isOwner && (
                        <button onClick={() => {
                          if (confirm(`Delete page "${page.display_name}"? This cannot be undone.`))
                            deletePage.mutate()
                          setShowMenu(false)
                        }} className="w-full text-left px-4 py-2 text-sm text-red-600 hover:bg-agora-50 dark:hover:bg-agora-700">
                          Delete page
                        </button>
                      )}
                      {!isOwner && (
                        <button onClick={() => { setShowReport(true); setShowMenu(false) }}
                          className="w-full text-left px-4 py-2 text-sm text-agora-700 dark:text-agora-200 hover:bg-agora-50 dark:hover:bg-agora-700 flex items-center gap-2">
                          <Flag size={13} /> Report page
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </div>
            </div>

            {/* Bio */}
            {page.bio && (
              <p className="text-sm text-agora-700 dark:text-agora-300 mt-3 leading-relaxed">{page.bio}</p>
            )}
          </div>
        </div>

        {/* Compose (owner only) */}
        {isOwner && showCompose && (
          <div className="card p-4 space-y-3">
            <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">
              Posting as <span className="text-agora-700 dark:text-agora-300">{page.display_name}</span>
            </p>
            <textarea
              value={composeContent}
              onChange={e => setComposeContent(e.target.value)}
              placeholder={`What's new on ${page.display_name}?`}
              rows={3}
              className="w-full resize-none bg-transparent text-sm text-agora-800 dark:text-agora-200 placeholder-agora-400 focus:outline-none"
            />
            {composeImageUrls.length > 0 && (
              <div className={`grid gap-1.5 ${composeImageUrls.length === 1 ? 'grid-cols-1' : 'grid-cols-2'}`}>
                {composeImageUrls.map((url, idx) => (
                  <div key={idx} className="relative">
                    <img src={url} alt="" className="rounded-lg w-full h-32 object-cover" />
                    <button onClick={() => setComposeImageUrls(prev => prev.filter((_, i) => i !== idx))}
                      className="absolute top-1 right-1 bg-black/60 text-white rounded-full w-5 h-5 flex items-center justify-center hover:bg-black/80">
                      <X size={10} />
                    </button>
                  </div>
                ))}
              </div>
            )}
            <div className="flex items-center gap-2 pt-2 border-t border-agora-100 dark:border-agora-700">
              <label className={`btn-ghost p-2 cursor-pointer ${composeImageUrls.length >= 4 ? 'opacity-40 pointer-events-none' : ''}`}>
                <Image size={18} />
                <input type="file" accept="image/*" multiple className="hidden" onChange={handleImageUpload}
                  disabled={composeUploading || composeImageUrls.length >= 4} />
              </label>
              <div className="ml-auto flex gap-2">
                <button onClick={() => { setShowCompose(false); setComposeContent(''); setComposeImageUrls([]) }}
                  className="btn-secondary text-sm">Cancel</button>
                <button onClick={() => createPagePost.mutate()} disabled={!canPost || createPagePost.isPending || composeUploading}
                  className="btn-primary text-sm">
                  {createPagePost.isPending ? 'Posting…' : 'Post'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Feed */}
        {feedLoading ? (
          <div className="text-center py-8 text-agora-400 text-sm">Loading posts…</div>
        ) : posts.length === 0 ? (
          <div className="card p-8 text-center space-y-2 text-agora-400">
            <BookOpen size={28} className="mx-auto opacity-40" />
            <p className="text-sm">No posts yet.</p>
            {isOwner && <p className="text-xs">Use the "New Post" button above to publish your first post.</p>}
          </div>
        ) : (
          <div className="space-y-3">
            {posts.map((post: any) => {
              const isOwnPost = user?.id === post.author_id
              const canDeletePost = isOwnPost || user?.role === 'admin' || user?.role === 'moderator'
              return (
              <div key={post.id} className="card p-4 space-y-2">
                {/* Post header */}
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                      {page.avatar_url
                        ? <img src={page.avatar_url} alt="" className="w-full h-full object-cover" />
                        : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-500">
                            {page.display_name[0]}
                          </span>}
                    </div>
                    <div className="min-w-0">
                      <p className="text-sm font-semibold truncate">{page.display_name}</p>
                      <p className="text-xs text-agora-400">
                        {new Date(post.created_at).toLocaleDateString(undefined, {
                          month: 'short', day: 'numeric', year: 'numeric',
                        })}
                      </p>
                    </div>
                  </div>

                  {/* Menu — edit/delete for the post's own author (or a site admin/mod) */}
                  {(isOwnPost || canDeletePost) && (
                    <div className="relative flex-shrink-0">
                      <button onClick={() => setOpenPostMenuId(id => id === post.id ? null : post.id)}
                        className="btn-ghost p-1 text-agora-400">
                        <MoreHorizontal size={16} />
                      </button>
                      {openPostMenuId === post.id && (
                        <div className="absolute right-0 top-6 z-10 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-700 rounded-lg shadow-lg py-1 min-w-[140px]"
                          onBlur={() => setOpenPostMenuId(null)}>
                          {isOwnPost && (
                            <button onClick={() => { setEditingPostId(post.id); setEditContent(post.content || ''); setOpenPostMenuId(null) }}
                              className="flex items-center gap-2 w-full px-3 py-2 text-sm text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-700">
                              <Pencil size={14} /> Edit
                            </button>
                          )}
                          {canDeletePost && (
                            <button onClick={() => { if (confirm('Delete post?')) deletePost.mutate(post.id); setOpenPostMenuId(null) }}
                              className="flex items-center gap-2 w-full px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20">
                              <Trash2 size={14} /> Delete
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>

                {/* Content */}
                {editingPostId === post.id ? (
                  <div className="space-y-2">
                    <textarea
                      value={editContent}
                      onChange={e => setEditContent(e.target.value)}
                      rows={3}
                      className="w-full resize-none bg-transparent text-sm text-agora-800 dark:text-agora-200 border border-agora-200 dark:border-agora-700 rounded-lg p-2 focus:outline-none"
                    />
                    <div className="flex gap-2 justify-end">
                      <button onClick={() => setEditingPostId(null)} className="btn-secondary text-sm">Cancel</button>
                      <button onClick={() => editPost.mutate(post.id)} disabled={!editContent.trim() || editPost.isPending}
                        className="btn-primary text-sm">
                        {editPost.isPending ? 'Saving…' : 'Save'}
                      </button>
                    </div>
                  </div>
                ) : (
                  <>
                    {post.content && <p className="text-sm text-agora-800 dark:text-agora-200 whitespace-pre-wrap">{post.content}</p>}
                    {post.image_url && (
                      <img src={post.image_url} alt="" className="rounded-lg w-full max-h-72 object-cover" />
                    )}
                  </>
                )}

                {/* Actions */}
                <div className="flex items-center gap-4 pt-1">
                  <button
                    onClick={() => post.liked ? unlikePost.mutate(post.id) : likePost.mutate(post.id)}
                    className={`flex items-center gap-1 text-xs ${post.liked ? 'text-red-500' : 'text-agora-400 hover:text-red-500'}`}>
                    <Heart size={14} fill={post.liked ? 'currentColor' : 'none'} />
                    <span>{post.like_count}</span>
                  </button>
                  <Link to={`/post/${post.id}`} className="flex items-center gap-1 text-xs text-agora-400 hover:text-agora-600">
                    <MessageCircle size={14} />
                    <span>{post.comment_count}</span>
                  </Link>
                </div>
              </div>
              )
            })}

            {/* Pagination */}
            <div className="flex justify-center gap-2 pb-2">
              {feedPage > 0 && (
                <button onClick={() => setFeedPage(p => p - 1)} className="btn-secondary text-sm">← Newer</button>
              )}
              {posts.length === 20 && (
                <button onClick={() => setFeedPage(p => p + 1)} className="btn-secondary text-sm">Older →</button>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  )
}
