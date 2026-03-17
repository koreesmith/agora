import { useState, useRef, useEffect } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { handle } from '../utils/handle'
import { isGifUrl } from '../utils/gif'
import { useQuery, useMutation, useQueryClient, useInfiniteQuery } from '@tanstack/react-query'
import { groupsApi, feedApi } from '../api'
import { useAuthStore } from '../store/auth'
import { formatDistanceToNow } from 'date-fns'
import { renderContent } from '../components/feed/CommentsSection'
import CommentsSection from '../components/feed/CommentsSection'
import { Heart, MessageCircle, Users, Lock, Globe, Settings, UserMinus, Shield, Image, X, Link2, Copy, Check, CheckCircle, XCircle, UserPlus, ClipboardList, BarChart2, Plus, Minus } from 'lucide-react'
import { REACTIONS, REACTION_MAP } from '../utils/reactions'
import CoverPhoto from '../components/common/CoverPhoto'

export default function GroupPage() {
  const { slug } = useParams<{ slug: string }>()!
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [tab, setTab] = useState<'feed'|'members'|'settings'>('feed')
  const [lightbox, setLightbox] = useState<string | null>(null)
  const [requestMsg, setRequestMsg] = useState('')
  const [showRequestModal, setShowRequestModal] = useState(false)
  const [requestSent, setRequestSent] = useState(false)

  const { data: groupData, isLoading, error } = useQuery({
    queryKey: ['group', slug],
    queryFn: () => groupsApi.get(slug!).then(r => r.data),
  })
  const group = groupData?.group

  const isOwner = group?.member_role === 'owner'
  const isMod   = group?.member_role === 'mod'
  const canManage = isOwner || isMod

  const join = useMutation({
    mutationFn: () => groupsApi.join(slug!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['group', slug] }),
  })
  const requestJoin = useMutation({
    mutationFn: () => groupsApi.requestJoin(slug!, requestMsg),
    onSuccess: () => { setShowRequestModal(false); setRequestSent(true) },
  })
  const leave = useMutation({
    mutationFn: () => groupsApi.leave(slug!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['group', slug] }),
  })
  const deleteGroup = useMutation({
    mutationFn: () => groupsApi.delete(slug!),
    onSuccess: () => navigate('/groups'),
  })

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (error || !group) return (
    <div className="card p-8 text-center space-y-2">
      <p className="font-semibold">Group not found</p>
      <Link to="/groups" className="text-sm text-agora-500 hover:underline">← Back to groups</Link>
    </div>
  )

  return (
    <>
    <div className="space-y-4">
      {/* Request to join modal */}
      {showRequestModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={() => setShowRequestModal(false)}>
          <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold">Request to Join</h2>
              <button onClick={() => setShowRequestModal(false)} className="btn-ghost p-1"><X size={18} /></button>
            </div>
            <p className="text-sm text-agora-500">This is a private group. Your request will be reviewed by the group admins.</p>
            <div>
              <label className="label">Message (optional)</label>
              <textarea className="input resize-none" autoComplete="off" rows={3} placeholder="Introduce yourself or explain why you'd like to join…"
                value={requestMsg} onChange={e => setRequestMsg(e.target.value)} />
            </div>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setShowRequestModal(false)} className="btn-secondary">Cancel</button>
              <button onClick={() => requestJoin.mutate()} disabled={requestJoin.isPending} className="btn-primary">
                {requestJoin.isPending ? 'Sending…' : 'Send Request'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Header */}
      <div className="card overflow-hidden">
        {group.cover_url
          ? <img src={group.cover_url} alt="" className="w-full h-32 object-cover cursor-zoom-in" onClick={() => setLightbox(group.cover_url)} />
          : <div className="w-full h-16 bg-gradient-to-r from-agora-600 to-agora-400" />}
        <div className="p-4 flex gap-3 items-start">
          <div className="w-14 h-14 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0 -mt-8 ring-2 ring-white dark:ring-agora-800">
            {group.avatar_url
              ? <img src={group.avatar_url} alt="" className="w-full h-full object-cover cursor-zoom-in" onClick={() => setLightbox(group.avatar_url)} />
              : <span className="w-full h-full flex items-center justify-center text-2xl font-bold text-agora-500">{group.name[0]}</span>}
          </div>
          <div className="flex-1 min-w-0">
            <h1 className="text-lg font-bold truncate">{group.name}</h1>
            <div className="flex items-center gap-2 text-xs text-agora-400 flex-wrap">
              {group.privacy === 'private' ? <Lock size={11} /> : <Globe size={11} />}
              <span className="capitalize">{group.privacy}</span>
              <span>·</span>
              <Users size={11} />
              <span>{group.member_count} {group.member_count === 1 ? 'member' : 'members'}</span>
              {group.member_role && (
                <><span>·</span>
                <span className="font-medium text-agora-600 dark:text-agora-400">
                  {group.member_role === 'owner' ? '👑 Owner' : group.member_role === 'mod' ? '🛡 Moderator' : 'Member'}
                </span></>
              )}
            </div>
          </div>
          <div className="flex-shrink-0 flex gap-2">
            {group.is_member
              ? <button onClick={() => { if (confirm(`Leave "${group.name}"?`)) leave.mutate() }} className="btn-secondary text-sm">Leave</button>
              : group.privacy === 'public'
                ? <button onClick={() => join.mutate()} disabled={join.isPending} className="btn-primary text-sm">Join Group</button>
                : requestSent
                  ? <span className="text-sm text-agora-400 flex items-center gap-1"><CheckCircle size={14} className="text-green-500" /> Request sent</span>
                  : <button onClick={() => setShowRequestModal(true)} className="btn-primary text-sm flex items-center gap-1.5"><UserPlus size={14} /> Request to Join</button>
            }
            {canManage && <button onClick={() => setTab('settings')} className="btn-ghost p-2"><Settings size={16} /></button>}
          </div>
        </div>
        {group.description && <p className="px-4 pb-4 text-sm text-agora-600 dark:text-agora-400">{group.description}</p>}

        {/* Tabs */}
        <div className="flex border-t border-agora-100 dark:border-agora-700">
          {[['feed','Feed'], ['members','Members'], ...(canManage ? [['settings','Settings']] : [])] .map(([id, label]) => (
            <button key={id} onClick={() => setTab(id as any)}
              className={`flex-1 py-2.5 text-sm font-medium transition-colors ${tab === id ? 'border-b-2 border-agora-600 text-agora-600' : 'text-agora-400 hover:text-agora-600'}`}>
              {label}
            </button>
          ))}
        </div>
      </div>

      {tab === 'feed' && <GroupFeed slug={slug!} group={group} />}
      {tab === 'members' && <GroupMembers slug={slug!} group={group} canManage={canManage} isOwner={isOwner} currentUserID={user?.id} />}
      {tab === 'settings' && canManage && <GroupSettings slug={slug!} group={group} isOwner={isOwner} onDelete={() => deleteGroup.mutate()} />}
    </div>
    {lightbox && (
      <div className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center" onClick={() => setLightbox(null)}>
        <button onClick={() => setLightbox(null)} className="absolute top-4 right-4 bg-black/40 text-white rounded-full p-1.5 hover:bg-black/70">
          <X size={20} />
        </button>
        <img src={lightbox} alt="" className="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl" onClick={e => e.stopPropagation()} />
      </div>
    )}
  </>
  )
}

// ── Group Feed ────────────────────────────────────────────────────────────────

function GroupFeed({ slug, group }: { slug: string, group: any }) {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [content, setContent] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [uploading, setUploading] = useState(false)
  const [openComments, setOpenComments] = useState<string|null>(null)
  const [openReactionPicker, setOpenReactionPicker] = useState<string|null>(null)
  const [pollEnabled, setPollEnabled] = useState(false)
  const [pollOptions, setPollOptions] = useState(['', ''])

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ['group-feed', slug],
    queryFn: ({ pageParam = 0 }) => groupsApi.getFeed(slug, pageParam).then(r => r.data),
    getNextPageParam: (last, pages) => last.posts?.length === 20 ? pages.length : undefined,
    initialPageParam: 0,
  })
  const posts = data?.pages.flatMap(p => p.posts) ?? []

  const createPost = useMutation({
    mutationFn: () => groupsApi.createPost(slug, {
      content,
      image_url: imageUrl,
      poll_options: pollEnabled ? pollOptions.filter(o => o.trim()) : [],
    }),
    onSuccess: () => {
      setContent(''); setImageUrl('')
      setPollEnabled(false); setPollOptions(['', ''])
      qc.invalidateQueries({ queryKey: ['group-feed', slug] })
    },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch (err: any) {
      const msg = err?.response?.data?.error || 'Upload failed. Please try a JPEG or PNG file.'
      alert(msg)
    }
    finally { setUploading(false) }
  }

  const reactPost = useMutation({
    mutationFn: ({ id, type, myReaction }: { id: string, type: string, myReaction: string }) =>
      myReaction === type ? feedApi.unreactPost(id) : feedApi.reactPost(id, type),
    onSettled: () => qc.invalidateQueries({ queryKey: ['group-feed', slug] }),
  })

  return (
    <div className="space-y-4">
      {/* Composer */}
      {group.is_member && (
        <div className="card p-4 space-y-3">
          <div className="flex gap-3">
            <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {user?.avatar_url
                ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-sm">{user?.username?.[0]?.toUpperCase()}</span>}
            </div>
            <textarea value={content} onChange={e => setContent(e.target.value)} placeholder={`Post something to ${group.name}…`}
              rows={2} autoComplete="off" className="flex-1 resize-none bg-transparent text-sm placeholder-agora-400 focus:outline-none" />
          </div>
          {imageUrl && (
            <div className="relative">
              <img src={imageUrl} alt="" className="rounded-lg w-full max-h-40 object-contain bg-agora-50 dark:bg-agora-900" />
              <button onClick={() => setImageUrl('')} className="absolute top-2 right-2 bg-black/60 text-white rounded-full w-6 h-6 flex items-center justify-center"><X size={12} /></button>
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
                    <button onClick={() => setPollOptions(opts => opts.filter((_, j) => j !== i))} className="text-agora-400 hover:text-red-500 transition-colors flex-shrink-0">
                      <Minus size={14} />
                    </button>
                  )}
                </div>
              ))}
              {pollOptions.length < 6 && (
                <button onClick={() => setPollOptions(opts => [...opts, ''])} className="flex items-center gap-1.5 text-xs text-agora-500 hover:text-agora-700 transition-colors">
                  <Plus size={12} /> Add option
                </button>
              )}
            </div>
          )}

          <div className="flex items-center gap-2 pt-1 border-t border-agora-100 dark:border-agora-700">
            <label className="btn-ghost p-2 cursor-pointer"><Image size={16} />
              <input type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading || !!imageUrl} />
            </label>
            <button
              onClick={() => { setPollEnabled(v => !v); if (pollEnabled) setPollOptions(['', '']) }}
              className={`flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs font-medium border transition-colors ${
                pollEnabled
                  ? 'bg-agora-100 dark:bg-agora-700 border-agora-400 text-agora-700 dark:text-agora-200'
                  : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-agora-400 hover:text-agora-600'
              }`}
            >
              <BarChart2 size={13} /> Poll
            </button>
            <button
              onClick={() => createPost.mutate()}
              disabled={
                (!content.trim() && !imageUrl && !(pollEnabled && pollOptions.filter(o => o.trim()).length >= 2))
                || createPost.isPending || uploading
                || (pollEnabled && pollOptions.filter(o => o.trim()).length < 2)
              }
              className="ml-auto btn-primary text-sm"
            >
              {createPost.isPending ? 'Posting…' : 'Post'}
            </button>
          </div>
        </div>
      )}

      {posts.length === 0 && !isFetchingNextPage && (
        <div className="card p-10 text-center text-agora-400">
          <p className="font-medium">No posts yet</p>
          {group.is_member && <p className="text-sm mt-1">Be the first to post something!</p>}
          {!group.is_member && group.privacy === 'public' && <p className="text-sm mt-1">Join the group to start posting.</p>}
        </div>
      )}

      {posts.map((post: any) => (
        <div key={post.id} className="card p-4 space-y-2">
          <div className="flex items-center gap-2.5">
            <Link to={`/profile/${post.username}`}>
              <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden">
                {post.avatar_url
                  ? <img src={post.avatar_url} alt="" className="w-full h-full object-cover" />
                  : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-sm">{(post.display_name||post.username)[0].toUpperCase()}</span>}
              </div>
            </Link>
            <div>
              <div className="flex items-center gap-1.5">
                <Link to={`/profile/${post.username}`} className="font-semibold text-sm hover:underline">{post.display_name || post.username}</Link>
                {post.author_role === 'owner' && <span className="text-xs bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 px-1.5 py-0.5 rounded-full">👑 Owner</span>}
                {post.author_role === 'mod'   && <span className="text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 px-1.5 py-0.5 rounded-full">🛡 Mod</span>}
              </div>
              <p className="text-xs text-agora-400">
                <Link to={`/post/${post.id}`} className="hover:underline">
                  {formatDistanceToNow(new Date(post.created_at), { addSuffix: true })}
                </Link>
              </p>
            </div>
          </div>

          {post.content && <p className="text-sm whitespace-pre-wrap break-words">{renderContent(post.content)}</p>}
          {post.image_url && (
            isGifUrl(post.image_url)
              ? <img src={post.image_url} alt="" className="rounded-lg w-full max-h-[32rem] object-contain" />
              : <img src={post.image_url} alt="" className="rounded-lg w-full max-h-[32rem] object-contain bg-agora-50 dark:bg-agora-900" />
          )}

          {/* Poll */}
          {post.poll_options?.length >= 2 && (() => {
            const opts: any[] = post.poll_options
            const totalVotes = opts.reduce((s: number, o: any) => s + o.votes, 0)
            const hasVoted = !!post.my_poll_vote
            return (
              <div className="space-y-2">
                {opts.map((opt: any) => {
                  const isMyVote = post.my_poll_vote === opt.id
                  const pct = totalVotes > 0 ? Math.round((opt.votes / totalVotes) * 100) : 0
                  return hasVoted ? (
                    <button key={opt.id}
                      onClick={() => feedApi.pollVote(post.id, isMyVote ? null as any : opt.id).then(() => qc.invalidateQueries({ queryKey: ['group-feed', slug] }))}
                      className={`w-full text-left rounded-lg overflow-hidden border transition-colors ${isMyVote ? 'border-agora-500 dark:border-agora-400' : 'border-agora-200 dark:border-agora-600'}`}
                    >
                      <div className="relative px-3 py-2">
                        <div className={`absolute inset-0 ${isMyVote ? 'bg-agora-100 dark:bg-agora-700' : 'bg-agora-50 dark:bg-agora-800/50'}`} style={{ width: `${pct}%` }} />
                        <div className="relative flex items-center justify-between gap-2">
                          <span className={`text-sm ${isMyVote ? 'font-semibold text-agora-800 dark:text-agora-100' : 'text-agora-700 dark:text-agora-300'}`}>
                            {isMyVote && <span className="mr-1">✓</span>}{opt.text}
                          </span>
                          <span className="text-xs text-agora-500 flex-shrink-0">{pct}%</span>
                        </div>
                      </div>
                    </button>
                  ) : (
                    <button key={opt.id}
                      onClick={() => feedApi.pollVote(post.id, opt.id).then(() => qc.invalidateQueries({ queryKey: ['group-feed', slug] }))}
                      className="w-full text-left px-3 py-2 rounded-lg border border-agora-200 dark:border-agora-600 hover:border-agora-500 dark:hover:border-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors text-sm text-agora-700 dark:text-agora-300"
                    >
                      {opt.text}
                    </button>
                  )
                })}
                <p className="text-xs text-agora-400">
                  {totalVotes} {totalVotes === 1 ? 'vote' : 'votes'}
                  {hasVoted && (
                    <button onClick={() => feedApi.pollUnvote(post.id).then(() => qc.invalidateQueries({ queryKey: ['group-feed', slug] }))} className="ml-2 underline hover:text-agora-600">
                      Remove vote
                    </button>
                  )}
                </p>
              </div>
            )
          })()}

          <div className="flex items-center gap-4 pt-1 border-t border-agora-100 dark:border-agora-700">
            {/* Reaction button */}
            <div className="relative">
              <button
                onClick={() => post.my_reaction ? reactPost.mutate({ id: post.id, type: post.my_reaction, myReaction: post.my_reaction }) : setOpenReactionPicker(openReactionPicker === post.id ? null : post.id)}
                onContextMenu={e => { e.preventDefault(); setOpenReactionPicker(openReactionPicker === post.id ? null : post.id) }}
                className={`flex items-center gap-1.5 text-sm transition-colors ${post.my_reaction ? 'text-red-500' : 'text-agora-400 hover:text-red-400'}`}
                title={post.my_reaction ? 'Click to remove · Right-click to change' : 'React'}
              >
                <span style={{ fontSize: 15 }}>{post.my_reaction ? REACTION_MAP[post.my_reaction]?.emoji || '❤️' : '🤍'}</span>
                {(post.reaction_count || 0) > 0 && <span>{post.reaction_count}</span>}
              </button>
              {openReactionPicker === post.id && (
                <div className="absolute bottom-8 left-0 z-20 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-2xl shadow-xl p-2 flex gap-1"
                  onMouseLeave={() => setOpenReactionPicker(null)}>
                  {REACTIONS.map(r => (
                    <button key={r.type} title={r.label}
                      onClick={() => { reactPost.mutate({ id: post.id, type: r.type, myReaction: post.my_reaction || '' }); setOpenReactionPicker(null) }}
                      className={`text-xl p-1 rounded-lg hover:bg-agora-100 dark:hover:bg-agora-700 transition-colors hover:scale-125 ${post.my_reaction === r.type ? 'bg-agora-100 dark:bg-agora-700' : ''}`}>
                      {r.emoji}
                    </button>
                  ))}
                </div>
              )}
            </div>
            <button onClick={() => setOpenComments(openComments === post.id ? null : post.id)}
              className="flex items-center gap-1.5 text-sm text-agora-400 hover:text-agora-600">
              <MessageCircle size={15} />
              {post.comment_count > 0 && <span>{post.comment_count}</span>}
              <span>Comment</span>
            </button>
          </div>

          {openComments === post.id && (
            <CommentsSection postId={post.id} postAuthorId={post.author_id} />
          )}
        </div>
      ))}

      {hasNextPage && (
        <button onClick={() => fetchNextPage()} disabled={isFetchingNextPage} className="w-full btn-secondary text-sm">
          {isFetchingNextPage ? 'Loading…' : 'Load more'}
        </button>
      )}
    </div>
  )
}

// ── Members Panel ─────────────────────────────────────────────────────────────

function GroupMembers({ slug, group, canManage, isOwner, currentUserID }: {
  slug: string, group: any, canManage: boolean, isOwner: boolean, currentUserID?: string
}) {
  const qc = useQueryClient()

  const { data } = useQuery({
    queryKey: ['group-members', slug],
    queryFn: () => groupsApi.listMembers(slug).then(r => r.data),
  })
  const members: any[] = data?.members ?? []

  const setRole = useMutation({
    mutationFn: ({ uid, role }: { uid: string, role: string }) => groupsApi.setRole(slug, uid, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['group-members', slug] }),
  })
  const remove = useMutation({
    mutationFn: (uid: string) => groupsApi.removeMember(slug, uid),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['group-members', slug] }); qc.invalidateQueries({ queryKey: ['group', slug] }) },
  })

  const roleLabel: Record<string, string> = { owner: '👑 Owner', mod: '🛡 Mod', member: 'Member' }

  return (
    <div className="card divide-y divide-agora-100 dark:divide-agora-700">
      {members.map(m => (
        <div key={m.id} className="flex items-center gap-3 p-3">
          <Link to={`/profile/${m.username}`}>
            <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden">
              {m.avatar_url
                ? <img src={m.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-sm">{(m.display_name||m.username)[0].toUpperCase()}</span>}
            </div>
          </Link>
          <div className="flex-1 min-w-0">
            <Link to={`/profile/${m.username}`} className="font-medium text-sm hover:underline">{m.display_name || m.username}</Link>
            <p className="text-xs text-agora-400">{handle(m.username, m.is_remote, m.remote_instance)} · {roleLabel[m.role]}</p>
          </div>
          {canManage && m.id !== currentUserID && m.role !== 'owner' && (
            <div className="flex items-center gap-1">
              {isOwner && (
                <button onClick={() => setRole.mutate({ uid: m.id, role: m.role === 'mod' ? 'member' : 'mod' })}
                  className="btn-ghost p-1.5 text-agora-400 hover:text-blue-500" title={m.role === 'mod' ? 'Remove mod' : 'Make mod'}>
                  <Shield size={14} />
                </button>
              )}
              <button onClick={() => { if (confirm(`Remove ${m.display_name || m.username}?`)) remove.mutate(m.id) }}
                className="btn-ghost p-1.5 text-agora-400 hover:text-red-500">
                <UserMinus size={14} />
              </button>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

// ── Settings Panel ────────────────────────────────────────────────────────────

function GroupSettings({ slug, group, isOwner, onDelete }: { slug: string, group: any, isOwner: boolean, onDelete: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState(group.name)
  const [description, setDescription] = useState(group.description)
  const [privacy, setPrivacy] = useState(group.privacy)
  const [msg, setMsg] = useState('')
  const [addMsg, setAddMsg] = useState('')
  const [copiedToken, setCopiedToken] = useState('')

  const invalidate = () => qc.invalidateQueries({ queryKey: ['group', slug] })

  const uploadGroupCover = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]; if (!f) return
    try {
      const res = await feedApi.uploadMedia(f, 'cover')
      await groupsApi.update(slug, { cover_url: res.data.url })
      invalidate()
    } catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
  }

  const saveCoverPosition = async (pos: string) => {
    await groupsApi.update(slug, { cover_position: pos })
    invalidate()
  }

  const uploadGroupAvatar = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]; if (!f) return
    try {
      const res = await feedApi.uploadMedia(f, 'avatar')
      await groupsApi.update(slug, { avatar_url: res.data.url })
      invalidate()
    } catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
  }

  const save = useMutation({
    mutationFn: () => groupsApi.update(slug, { name, description, ...(isOwner ? { privacy } : {}) }),
    onSuccess: () => { setMsg('Saved!'); invalidate(); setTimeout(() => setMsg(''), 2000) },
  })

  // Invite links
  const { data: inviteData, refetch: refetchInvites } = useQuery({
    queryKey: ['group-invites', slug],
    queryFn: () => groupsApi.listInvites(slug).then(r => r.data),
  })
  const invites: any[] = inviteData?.invites ?? []

  const createInvite = useMutation({
    mutationFn: () => groupsApi.createInvite(slug),
    onSuccess: () => refetchInvites(),
  })
  const revokeInvite = useMutation({
    mutationFn: (token: string) => groupsApi.revokeInvite(slug, token),
    onSuccess: () => refetchInvites(),
  })

  const copyInvite = (token: string) => {
    const url = `${window.location.origin}/invite/${token}`
    navigator.clipboard.writeText(url)
    setCopiedToken(token)
    setTimeout(() => setCopiedToken(''), 2000)
  }

  // Join requests
  const { data: reqData, refetch: refetchRequests } = useQuery({
    queryKey: ['group-requests', slug],
    queryFn: () => groupsApi.listRequests(slug).then(r => r.data),
  })
  const requests: any[] = reqData?.requests ?? []

  const approveReq = useMutation({
    mutationFn: (id: string) => groupsApi.approveRequest(slug, id),
    onSuccess: () => { refetchRequests(); qc.invalidateQueries({ queryKey: ['group', slug] }) },
  })
  const rejectReq = useMutation({
    mutationFn: (id: string) => groupsApi.rejectRequest(slug, id),
    onSuccess: () => refetchRequests(),
  })

  // Direct add
  // (handled by AddMemberTypeahead component below)

  return (
    <div className="space-y-4">

      {/* Group photos */}
      <div className="card p-4 space-y-3">
        <h3 className="font-semibold">Group Photos</h3>

        {/* Cover photo */}
        <div>
          <label className="label mb-1.5">Cover photo</label>
          <div className="rounded-xl overflow-hidden">
            <CoverPhoto
              src={group.cover_url}
              position={group.cover_position || '50% 50%'}
              height="h-36"
              editable={true}
              onUpload={uploadGroupCover}
              onPositionSave={saveCoverPosition}
              clickable={false}
            />
          </div>
        </div>

        {/* Group avatar */}
        <div className="flex items-center gap-3">
          <div className="w-14 h-14 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
            {group.avatar_url
              ? <img src={group.avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center text-xl font-bold text-agora-600">{group.name[0].toUpperCase()}</span>}
          </div>
          <label className="btn-secondary text-sm cursor-pointer">
            Change group avatar
            <input type="file" accept="image/*" className="hidden" onChange={uploadGroupAvatar} />
          </label>
        </div>
      </div>

      {/* Join Requests */}
      {requests.length > 0 && (
        <div className="card p-4 space-y-3">
          <h3 className="font-semibold flex items-center gap-2">
            <ClipboardList size={16} />
            Join Requests
            <span className="ml-auto bg-agora-600 text-white text-xs font-bold px-2 py-0.5 rounded-full">{requests.length}</span>
          </h3>
          <div className="divide-y divide-agora-100 dark:divide-agora-700">
            {requests.map((req: any) => (
              <div key={req.id} className="flex items-center gap-3 py-2.5">
                <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                  {req.avatar_url
                    ? <img src={req.avatar_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-xs">{(req.display_name||req.username)[0].toUpperCase()}</span>}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium">{req.display_name || req.username}</p>
                  {req.message && <p className="text-xs text-agora-400 truncate">"{req.message}"</p>}
                </div>
                <div className="flex gap-1.5">
                  <button onClick={() => approveReq.mutate(req.id)} className="btn-ghost p-1.5 text-green-500 hover:bg-green-50 dark:hover:bg-green-900/20" title="Approve">
                    <CheckCircle size={16} />
                  </button>
                  <button onClick={() => rejectReq.mutate(req.id)} className="btn-ghost p-1.5 text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20" title="Reject">
                    <XCircle size={16} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Invite Links */}
      <div className="card p-4 space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold flex items-center gap-2"><Link2 size={16} /> Invite Links</h3>
          <button onClick={() => createInvite.mutate()} disabled={createInvite.isPending} className="btn-primary text-xs py-1 px-2.5">
            + New Link
          </button>
        </div>
        {invites.length === 0 && (
          <p className="text-sm text-agora-400">No invite links yet. Create one to share with people.</p>
        )}
        {invites.map((inv: any) => (
          <div key={inv.id} className="flex items-center gap-2 bg-agora-50 dark:bg-agora-700/50 rounded-lg px-3 py-2">
            <code className="text-xs text-agora-600 dark:text-agora-300 flex-1 truncate">
              {window.location.origin}/invite/{inv.token}
            </code>
            <span className="text-xs text-agora-400 flex-shrink-0">{inv.uses}{inv.max_uses > 0 ? `/${inv.max_uses}` : ''} uses</span>
            <button onClick={() => copyInvite(inv.token)} className="btn-ghost p-1.5 flex-shrink-0" title="Copy link">
              {copiedToken === inv.token ? <Check size={13} className="text-green-500" /> : <Copy size={13} />}
            </button>
            <button onClick={() => { if (confirm('Revoke this invite link?')) revokeInvite.mutate(inv.token) }}
              className="btn-ghost p-1.5 flex-shrink-0 text-red-400" title="Revoke">
              <X size={13} />
            </button>
          </div>
        ))}
      </div>

      {/* Add member directly */}
      <div className="card p-4 space-y-3">
        <h3 className="font-semibold flex items-center gap-2"><UserPlus size={16} /> Add Member Directly</h3>
        {addMsg && <p className={`text-sm ${addMsg.includes('added') ? 'text-green-600' : 'text-red-500'}`}>{addMsg}</p>}
        <AddMemberTypeahead slug={slug} onAdded={() => {
          qc.invalidateQueries({ queryKey: ['group-members', slug] })
          qc.invalidateQueries({ queryKey: ['group', slug] })
          setAddMsg('Member added!')
          setTimeout(() => setAddMsg(''), 2000)
        }} onError={(e) => setAddMsg(e)} />
      </div>

      {/* Group settings */}
      <div className="card p-4 space-y-4">
        <h3 className="font-semibold">Group Settings</h3>
        {msg && <p className="text-sm text-green-600">{msg}</p>}
        <div><label className="label">Name</label><input className="input" autoComplete="off" value={name} onChange={e => setName(e.target.value)} /></div>
        <div><label className="label">Description</label><textarea className="input resize-none" autoComplete="off" rows={3} value={description} onChange={e => setDescription(e.target.value)} /></div>
        {isOwner && (
          <div>
            <label className="label">Privacy</label>
            <div className="grid grid-cols-2 gap-2 mt-1">
              {(['public', 'private'] as const).map(p => (
                <button key={p} onClick={() => setPrivacy(p)}
                  className={`p-2.5 rounded-lg border-2 text-sm font-medium flex items-center gap-1.5 ${privacy === p ? 'border-agora-600 bg-agora-50 dark:bg-agora-700' : 'border-agora-200 dark:border-agora-600'}`}>
                  {p === 'public' ? <Globe size={13}/> : <Lock size={13}/>}
                  <span className="capitalize">{p}</span>
                </button>
              ))}
            </div>
          </div>
        )}
        <button onClick={() => save.mutate()} disabled={save.isPending} className="btn-primary">{save.isPending ? 'Saving…' : 'Save changes'}</button>
      </div>

      {isOwner && (
        <div className="card p-4 space-y-3 border-red-200 dark:border-red-800">
          <h3 className="font-semibold text-red-600">Danger Zone</h3>
          <p className="text-sm text-agora-500">Deleting this group is permanent. All posts will be removed.</p>
          <button onClick={() => { if (confirm(`Delete "${group.name}"? This cannot be undone.`)) onDelete() }} className="btn-danger">
            Delete Group
          </button>
        </div>
      )}
    </div>
  )
}

// ── Add Member Typeahead ───────────────────────────────────────────────────────

function AddMemberTypeahead({ slug, onAdded, onError }: {
  slug: string, onAdded: () => void, onError: (msg: string) => void
}) {
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [selected, setSelected] = useState<{ username: string, display_name: string, avatar_url: string } | null>(null)
  const [open, setOpen] = useState(false)
  const [adding, setAdding] = useState(false)
  const debounceTimer = useRef<ReturnType<typeof setTimeout>>()
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => setDebouncedQuery(query), 250)
    return () => clearTimeout(debounceTimer.current)
  }, [query])

  const { data, isFetching } = useQuery({
    queryKey: ['group-member-search', slug, debouncedQuery],
    queryFn: () => groupsApi.memberSearch(slug, debouncedQuery).then(r => r.data),
    enabled: debouncedQuery.length >= 1 && !selected,
  })
  const suggestions: any[] = data?.users ?? []

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const handleSelect = (u: any) => {
    setSelected(u)
    setQuery(u.display_name || u.username)
    setOpen(false)
  }

  const handleAdd = async () => {
    if (!selected) return
    setAdding(true)
    try {
      await groupsApi.addMember(slug, selected.username)
      onAdded()
      setSelected(null)
      setQuery('')
    } catch (e: any) {
      onError(e.response?.data?.error || 'Could not add member')
    } finally {
      setAdding(false)
    }
  }

  const handleInputChange = (val: string) => {
    setQuery(val)
    setSelected(null)
    setOpen(true)
  }

  return (
    <div className="flex gap-2" ref={containerRef}>
      <div className="relative flex-1">
        <input
          className="input text-sm w-full"
          placeholder="Search by name or username…"
          value={query}
          onChange={e => handleInputChange(e.target.value)}
          onFocus={() => { if (query && !selected) setOpen(true) }}
          onKeyDown={e => { if (e.key === 'Escape') setOpen(false) }}
          autoComplete="off"
        />
        {selected && (
          <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
            {selected.avatar_url
              ? <img src={selected.avatar_url} className="w-5 h-5 rounded-full object-cover" alt="" />
              : null}
            <button onClick={() => { setSelected(null); setQuery('') }}
              className="text-agora-400 hover:text-agora-600 ml-1">
              <X size={13} />
            </button>
          </div>
        )}

        {open && !selected && debouncedQuery.length >= 1 && (
          <div className="absolute z-20 top-full left-0 right-0 mt-1 bg-white dark:bg-agora-800 rounded-lg shadow-lg border border-agora-100 dark:border-agora-700 overflow-hidden max-h-52 overflow-y-auto">
            {isFetching && (
              <div className="px-3 py-2 text-xs text-agora-400">Searching…</div>
            )}
            {!isFetching && suggestions.length === 0 && (
              <div className="px-3 py-2 text-xs text-agora-400">No users found</div>
            )}
            {suggestions.map((u: any) => (
              <button key={u.id} onMouseDown={() => handleSelect(u)}
                className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left transition-colors">
                <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0">
                  {u.avatar_url
                    ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-500">
                        {(u.display_name || u.username)[0].toUpperCase()}
                      </span>}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium truncate">{u.display_name || u.username}</p>
                  <p className="text-xs text-agora-400 truncate">{handle(u.username, u.is_remote, u.remote_instance)}{u.is_friend ? ' · friend' : ''}</p>
                </div>
                {u.is_friend && (
                  <span className="text-xs text-agora-500 flex-shrink-0">👥</span>
                )}
              </button>
            ))}
          </div>
        )}
      </div>

      <button
        onClick={handleAdd}
        disabled={!selected || adding}
        className="btn-primary text-sm px-4 flex-shrink-0"
      >
        {adding ? '…' : 'Add'}
      </button>
    </div>
  )
}
