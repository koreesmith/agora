import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { usersApi, feedApi, friendsApi, albumsApi, dmApi, blocksApi, federationApi, atprotoApi } from '../api'
import { useAuthStore } from '../store/auth'
import { useChatStore } from '../store/chat'
import PostCard from '../components/feed/PostCard'
import { renderContent } from '../components/feed/CommentsSection'
import { handle } from '../utils/handle'
import { UserPlus, UserCheck, UserX, Clock, Lock, FileText, Images, Globe, Users, X, Bell, BellOff, PenLine, CheckCircle, XCircle, MessageCircle, ShieldOff, Shield } from 'lucide-react'
import FriendListModal from '../components/common/FriendListModal'

const visIcon: Record<string, React.ReactNode> = {
  public:  <Globe size={10} />,
  friends: <Users size={10} />,
  private: <Lock  size={10} />,
}

export default function ProfilePage() {
  const { username } = useParams<{ username: string }>()
  const { user: me } = useAuthStore()
  const { openChat } = useChatStore()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'posts'|'photos'|'wall'>('posts')
  const [lightbox, setLightbox] = useState<string | null>(null)
  const [showWallComposer, setShowWallComposer] = useState(false)
  const [wallContent, setWallContent] = useState('')
  const [wallPosting, setWallPosting] = useState(false)

  const { data: profile, isLoading } = useQuery({
    queryKey: ['profile', username],
    queryFn: () => usersApi.getProfile(username!).then(r => r.data),
  })

  const isSelf = me?.id === profile?.id || me?.username === username
  const canSeeTimeline = isSelf || (!profile?.hide_timeline && (!profile?.profile_private || profile?.friend_status === 'accepted'))

  const { data: postsData } = useQuery({
    queryKey: ['user-posts', username],
    queryFn: () => feedApi.getUserPosts(username!).then(r => r.data),
    enabled: !!profile && canSeeTimeline && tab === 'posts',
  })

  // Photos/Wall require auth — the tabs are hidden for guests, but also
  // gate the queries so a stale tab state can't fire an unauthenticated request.
  const { data: albumsData } = useQuery({
    queryKey: ['user-albums', username],
    queryFn: () => albumsApi.listForUser(username!).then(r => r.data),
    enabled: !!profile && canSeeTimeline && tab === 'photos' && !!me,
  })

  const { data: wallData, refetch: refetchWall } = useQuery({
    queryKey: ['wall', username],
    queryFn: () => feedApi.getWall(username!).then(r => r.data),
    enabled: !!profile && tab === 'wall' && !!me,
  })

  const { data: wallQueueData, refetch: refetchQueue } = useQuery({
    queryKey: ['wall-queue'],
    queryFn: () => feedApi.getWallQueue().then(r => r.data),
    enabled: !!profile && tab === 'wall' && profile.friend_status === 'self' && !!me,
  })

  const [listModalFriend, setListModalFriend] = useState<any | null>(null)

  const inv = () => {
    qc.invalidateQueries({ queryKey: ['profile', username] })
    qc.invalidateQueries({ queryKey: ['friends'] })
    qc.invalidateQueries({ queryKey: ['requests'] })
    qc.invalidateQueries({ queryKey: ['notifications'] })
  }

  const sendReq = useMutation({
    mutationFn: () => friendsApi.sendRequest(profile.id),
    onSuccess: () => { inv(); setListModalFriend(profile) },
  })
  const accept  = useMutation({
    mutationFn: () => friendsApi.acceptRequest(profile.id),
    onSuccess: () => { inv(); setListModalFriend(profile) },
  })
  const decline = useMutation({ mutationFn: () => friendsApi.declineRequest(profile.id), onSuccess: inv })
  const unfriend= useMutation({ mutationFn: () => friendsApi.unfriend(profile.id), onSuccess: inv })
  const toggleNotify = useMutation({
    mutationFn: () => profile.post_notifications_enabled
      ? usersApi.disablePostNotify(profile.username)
      : usersApi.enablePostNotify(profile.username),
    onSuccess: inv,
  })

  // AGORA-167: fediverse accounts have no friending concept — follow/notify
  // (ap_following) is the equivalent, surfaced here instead of Add friend.
  const followFed = useMutation({
    mutationFn: () => federationApi.followFediverseAccount(profile.ap_actor_url),
    onSuccess: inv,
  })
  const unfollowFed = useMutation({
    mutationFn: () => federationApi.unfollowFediverseAccount(profile.follow_id),
    onSuccess: inv,
  })
  const toggleFedNotify = useMutation({
    mutationFn: () => federationApi.toggleFollowNotify(profile.follow_id, !profile.follow_notify),
    onSuccess: inv,
  })

  // AGORA-234: native Bluesky accounts have no friending concept either —
  // follow/notify (at_following) is the equivalent, same as the fediverse
  // block above but against the AT Proto endpoints. profile.username is the
  // account's Bluesky handle for these rows (getOrCreateRemoteATUser stores
  // the handle as the username), so it doubles as the "actor" to follow.
  const followBsky = useMutation({
    mutationFn: () => atprotoApi.followBlueskyAccount(profile.username),
    onSuccess: inv,
  })
  const unfollowBsky = useMutation({
    mutationFn: () => atprotoApi.unfollowBlueskyAccount(profile.follow_id),
    onSuccess: inv,
  })
  const toggleBskyNotify = useMutation({
    mutationFn: () => atprotoApi.toggleFollowNotify(profile.follow_id, !profile.follow_notify),
    onSuccess: inv,
  })

  const wallApprove = useMutation({
    mutationFn: (id: string) => feedApi.wallApprove(id),
    onSuccess: () => { refetchWall(); refetchQueue() },
  })
  const wallReject = useMutation({
    mutationFn: (id: string) => feedApi.wallReject(id),
    onSuccess: () => { refetchWall(); refetchQueue() },
  })

  const startDM = useMutation({
    mutationFn: () => dmApi.startConversation(profile.username),
    onSuccess: (res) => openChat(res.data.id),
  })

  const toggleBlock = useMutation({
    mutationFn: () => profile.is_blocked
      ? blocksApi.unblock(profile.username)
      : blocksApi.block(profile.username),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile', username] })
      qc.invalidateQueries({ queryKey: ['friends'] })
    },
  })

  const handlePostToWall = async () => {
    if (!wallContent.trim()) return
    setWallPosting(true)
    try {
      await feedApi.createPost({ content: wallContent, wall_user_id: profile.id })
      setWallContent('')
      setShowWallComposer(false)
      refetchWall()
    } finally {
      setWallPosting(false)
    }
  }

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (!profile)  return <div className="text-center py-12 text-agora-400">User not found.</div>

  const status = profile.friend_status
  const isFediverse = !!profile.ap_actor_url
  const isBluesky = profile.remote_instance === 'bsky.app'
  const canSeeContent = isSelf || (!profile.hide_timeline && (!profile.profile_private || status === 'accepted'))

  const albums: any[] = albumsData?.albums ?? []
  const posts: any[]  = postsData?.posts ?? []

  return (
    <>
    {listModalFriend && (
      <FriendListModal
        friend={listModalFriend}
        onClose={() => setListModalFriend(null)}
      />
    )}
    <div className="space-y-4">
      {/* Cover + avatar */}
      <div className="card overflow-hidden">
        <div className="h-32 bg-gradient-to-r from-agora-300 to-agora-500 dark:from-agora-700 dark:to-agora-900">
          {profile.cover_url && (
            <img
              src={profile.cover_url}
              alt=""
              className="w-full h-full object-cover cursor-zoom-in"
              onClick={() => setLightbox(profile.cover_url)}
            />
          )}
        </div>
        <div className="px-4 pb-4">
          <div className="flex items-end justify-between -mt-10 mb-3">
            <div className="w-20 h-20 rounded-full border-4 border-white dark:border-agora-800 bg-agora-200 dark:bg-agora-700 overflow-hidden">
              {profile.avatar_url
                ? <img src={profile.avatar_url} alt="" className="w-full h-full object-cover cursor-zoom-in" onClick={() => setLightbox(profile.avatar_url)} />
                : <span className="w-full h-full flex items-center justify-center text-2xl font-bold text-agora-600">{profile.display_name?.[0]?.toUpperCase()}</span>
              }
            </div>
            <div className="flex gap-2 mt-10">
              {!me && !isSelf && (
                <Link to="/login" className="btn-primary text-sm flex items-center gap-1">
                  <UserPlus size={16}/> Sign in to interact
                </Link>
              )}
              {me && !isSelf && isFediverse && (
                <div className="flex gap-2">
                  <button
                    onClick={() => { if (profile.following) { if (confirm(`Unfollow ${profile.display_name}?`)) unfollowFed.mutate() } else followFed.mutate() }}
                    disabled={followFed.isPending || unfollowFed.isPending}
                    className={profile.following ? 'btn-secondary text-sm flex items-center gap-1' : 'btn-primary text-sm flex items-center gap-1'}
                  >
                    {profile.following ? <><UserCheck size={16}/> Following</> : <><UserPlus size={16}/> Follow</>}
                  </button>
                  {profile.following && (
                    <button
                      onClick={() => toggleFedNotify.mutate()}
                      disabled={toggleFedNotify.isPending}
                      title={profile.follow_notify ? 'Turn off notifications for this account' : 'Notify me when they post'}
                      className={`btn-secondary text-sm flex items-center gap-1 ${profile.follow_notify ? 'text-agora-700 dark:text-agora-200 border-agora-400' : 'text-agora-400'}`}
                    >
                      {profile.follow_notify ? <><BellOff size={15}/> Notifying</> : <><Bell size={15}/> Notify me</>}
                    </button>
                  )}
                  <button
                    onClick={() => { if (confirm(profile.is_blocked ? `Unblock ${profile.display_name}?` : `Block ${profile.display_name}? They won't be able to see your profile or contact you.`)) toggleBlock.mutate() }}
                    disabled={toggleBlock.isPending}
                    className="btn-secondary text-sm flex items-center gap-1 text-agora-400"
                    title={profile.is_blocked ? 'Unblock' : 'Block'}
                  >
                    {profile.is_blocked ? <ShieldOff size={15}/> : <Shield size={15}/>}
                    {profile.is_blocked ? 'Unblock' : 'Block'}
                  </button>
                </div>
              )}
              {me && !isSelf && isBluesky && (
                <div className="flex gap-2">
                  <button
                    onClick={() => { if (profile.following) { if (confirm(`Unfollow ${profile.display_name}?`)) unfollowBsky.mutate() } else followBsky.mutate() }}
                    disabled={followBsky.isPending || unfollowBsky.isPending}
                    className={profile.following ? 'btn-secondary text-sm flex items-center gap-1' : 'btn-primary text-sm flex items-center gap-1'}
                  >
                    {profile.following ? <><UserCheck size={16}/> Following</> : <><UserPlus size={16}/> Follow</>}
                  </button>
                  {profile.following && (
                    <button
                      onClick={() => toggleBskyNotify.mutate()}
                      disabled={toggleBskyNotify.isPending}
                      title={profile.follow_notify ? 'Turn off notifications for this account' : 'Notify me when they post'}
                      className={`btn-secondary text-sm flex items-center gap-1 ${profile.follow_notify ? 'text-agora-700 dark:text-agora-200 border-agora-400' : 'text-agora-400'}`}
                    >
                      {profile.follow_notify ? <><BellOff size={15}/> Notifying</> : <><Bell size={15}/> Notify me</>}
                    </button>
                  )}
                  <button
                    onClick={() => { if (confirm(profile.is_blocked ? `Unblock ${profile.display_name}?` : `Block ${profile.display_name}? They won't be able to see your profile or contact you.`)) toggleBlock.mutate() }}
                    disabled={toggleBlock.isPending}
                    className="btn-secondary text-sm flex items-center gap-1 text-agora-400"
                    title={profile.is_blocked ? 'Unblock' : 'Block'}
                  >
                    {profile.is_blocked ? <ShieldOff size={15}/> : <Shield size={15}/>}
                    {profile.is_blocked ? 'Unblock' : 'Block'}
                  </button>
                </div>
              )}
              {me && !isSelf && !isFediverse && !isBluesky && !status && (
                <div className="flex gap-2">
                  <button onClick={() => sendReq.mutate()} className="btn-primary text-sm flex items-center gap-1">
                    <UserPlus size={16}/> Add friend
                  </button>
                  <button
                    onClick={() => { if (confirm(profile.is_blocked ? `Unblock ${profile.display_name}?` : `Block ${profile.display_name}? They won't be able to see your profile or contact you.`)) toggleBlock.mutate() }}
                    disabled={toggleBlock.isPending}
                    className="btn-secondary text-sm flex items-center gap-1 text-agora-400"
                    title={profile.is_blocked ? 'Unblock' : 'Block'}
                  >
                    {profile.is_blocked ? <ShieldOff size={15}/> : <Shield size={15}/>}
                    {profile.is_blocked ? 'Unblock' : 'Block'}
                  </button>
                </div>
              )}
              {!isSelf && status === 'pending' && (
                <button disabled className="btn-secondary text-sm flex items-center gap-1"><Clock size={16}/> Pending</button>
              )}
              {!isSelf && status === 'pending_incoming' && (
                <div className="flex gap-2">
                  <button onClick={() => accept.mutate()} className="btn-primary text-sm flex items-center gap-1">
                    <UserCheck size={16}/> Accept request
                  </button>
                  <button onClick={() => { if(confirm('Decline request?')) decline.mutate() }} className="btn-secondary text-sm flex items-center gap-1">
                    <UserX size={16}/> Decline
                  </button>
                </div>
              )}
              {!isSelf && status === 'accepted' && (
                <div className="flex gap-2 flex-wrap">
                  <button onClick={() => { if(confirm('Unfriend?')) unfriend.mutate() }} className="btn-secondary text-sm flex items-center gap-1">
                    <UserCheck size={16}/> Friends
                  </button>
                  <button
                    onClick={() => startDM.mutate()}
                    disabled={startDM.isPending}
                    className="btn-secondary text-sm flex items-center gap-1"
                  >
                    <MessageCircle size={15}/> Message
                  </button>
                  <button
                    onClick={() => toggleNotify.mutate()}
                    disabled={toggleNotify.isPending}
                    title={profile.post_notifications_enabled ? 'Turn off post notifications' : 'Notify me when they post'}
                    className={`btn-secondary text-sm flex items-center gap-1 ${profile.post_notifications_enabled ? 'text-agora-700 dark:text-agora-200 border-agora-400' : 'text-agora-400'}`}
                  >
                    {profile.post_notifications_enabled
                      ? <><BellOff size={15}/> Notifying</>
                      : <><Bell size={15}/> Notify me</>
                    }
                  </button>
                  <button
                    onClick={() => { setTab('wall'); setShowWallComposer(true) }}
                    className="btn-primary text-sm flex items-center gap-1"
                  >
                    <PenLine size={15}/> Write on wall
                  </button>
                  <button
                    onClick={() => { if (confirm(`Block ${profile.display_name}? This will also unfriend them.`)) toggleBlock.mutate() }}
                    disabled={toggleBlock.isPending}
                    className="btn-secondary text-sm flex items-center gap-1 text-red-400"
                    title="Block"
                  >
                    <Shield size={15}/> Block
                  </button>
                </div>
              )}
            </div>
          </div>
          <h1 className="text-xl font-bold">
            {profile.display_name}
            {profile.pronouns && (
              <span className="text-agora-400 dark:text-agora-500 text-base font-normal ml-2">({profile.pronouns})</span>
            )}
          </h1>
          <div className="flex items-center gap-2">
            <p className="text-agora-500 text-sm">{handle(profile.username, profile.is_remote, profile.remote_instance)}</p>
            {/* AGORA-249: fediverse/Bluesky mutual-follow indicator — meaningful
                regardless of whether the viewer follows them back. */}
            {(isFediverse || isBluesky) && profile.follows_back && (
              <span title="Follows you" className="flex items-center gap-1 text-xs text-agora-600 dark:text-agora-300 bg-agora-100 dark:bg-agora-700 rounded-full px-2 py-0.5">
                <UserCheck size={11} /> Follows you
              </span>
            )}
          </div>
          {profile.bio && <p className="text-sm mt-2 text-agora-700 dark:text-agora-300 whitespace-pre-wrap break-words">{renderContent(profile.bio)}</p>}
          <div className="flex items-center gap-4 mt-3 text-sm text-agora-500">
            {/* AGORA-253: a remote account's own post/follower/following
                counts, same layout Bluesky itself shows on a profile — in
                place of "friends", which Agora has no concept of for an
                account it doesn't actually track the social graph of. */}
            {(isFediverse || isBluesky) && profile.remote_follower_count != null ? (
              <>
                <span><strong className="text-agora-800 dark:text-agora-200">{profile.remote_post_count ?? 0}</strong> posts</span>
                <span><strong className="text-agora-800 dark:text-agora-200">{profile.remote_follower_count ?? 0}</strong> followers</span>
                <span><strong className="text-agora-800 dark:text-agora-200">{profile.remote_following_count ?? 0}</strong> following</span>
              </>
            ) : (
              <span><strong className="text-agora-800 dark:text-agora-200">{profile.friend_count || 0}</strong> friends</span>
            )}
            {profile.location && <span>{profile.location}</span>}
            {profile.website && <a href={profile.website} className="text-agora-600 hover:underline" target="_blank" rel="noreferrer">{profile.website}</a>}
          </div>
        </div>

        {/* Tabs — Photos/Wall require auth, so guests only get Posts */}
        {canSeeContent && (
          <div className="flex border-t border-agora-100 dark:border-agora-700">
            <button onClick={() => setTab('posts')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-2.5 text-sm font-medium transition-colors ${tab === 'posts' ? 'border-b-2 border-agora-600 text-agora-600' : 'text-agora-400 hover:text-agora-600'}`}>
              <FileText size={14} /> Posts
            </button>
            {me && (
              <button onClick={() => setTab('photos')}
                className={`flex-1 flex items-center justify-center gap-1.5 py-2.5 text-sm font-medium transition-colors ${tab === 'photos' ? 'border-b-2 border-agora-600 text-agora-600' : 'text-agora-400 hover:text-agora-600'}`}>
                <Images size={14} /> Photos
              </button>
            )}
            {me && (
              <button onClick={() => setTab('wall')}
                className={`flex-1 flex items-center justify-center gap-1.5 py-2.5 text-sm font-medium transition-colors ${tab === 'wall' ? 'border-b-2 border-agora-600 text-agora-600' : 'text-agora-400 hover:text-agora-600'}`}>
                <PenLine size={14} /> Wall
              </button>
            )}
          </div>
        )}
      </div>

      {/* Private profile gate */}
      {!canSeeContent ? (
        <div className="card p-8 text-center text-agora-400">
          <Lock size={32} className="mx-auto mb-2" />
          {profile.hide_timeline
            ? <>
                <p className="font-medium">Timeline hidden</p>
                <p className="text-sm mt-1">{profile.display_name} has hidden their post timeline.</p>
              </>
            : <>
                <p className="font-medium">This profile is private</p>
                <p className="text-sm mt-1">Add {profile.display_name} as a friend to see their posts.</p>
              </>
          }
        </div>
      ) : tab === 'posts' ? (
        <div className="space-y-4">
          {posts.map((p: any) => <PostCard key={p.id} post={p} invalidateKey={`user-posts`} />)}
          {posts.length === 0 && <div className="card p-6 text-center text-agora-400 text-sm">No posts yet.</div>}
        </div>
      ) : tab === 'wall' ? (
        <div className="space-y-4">
          {/* Wall composer */}
          {!isSelf && status === 'accepted' && showWallComposer && (
            <div className="card p-4 space-y-3">
              <p className="text-sm font-medium text-agora-700 dark:text-agora-300">Write on {profile.display_name}'s wall</p>
              <textarea
                className="input w-full resize-none text-sm"
                rows={3}
                autoComplete="off"
                placeholder={`Write something on ${profile.display_name}'s wall…`}
                value={wallContent}
                onChange={e => setWallContent(e.target.value)}
                autoFocus
              />
              <div className="flex gap-2 justify-end">
                <button onClick={() => { setShowWallComposer(false); setWallContent('') }} className="btn-secondary text-sm">Cancel</button>
                <button onClick={handlePostToWall} disabled={!wallContent.trim() || wallPosting} className="btn-primary text-sm">
                  {wallPosting ? 'Posting…' : 'Post to wall'}
                </button>
              </div>
            </div>
          )}

          {/* Pending queue — wall owner only */}
          {isSelf && (wallQueueData?.posts || []).length > 0 && (
            <div className="card p-4 space-y-3">
              <h3 className="font-semibold text-sm text-agora-700 dark:text-agora-300 flex items-center gap-2">
                <Clock size={15} /> Pending approval ({wallQueueData.posts.length})
              </h3>
              {wallQueueData.posts.map((p: any) => (
                <div key={p.id} className="border border-agora-200 dark:border-agora-600 rounded-xl p-3 space-y-2">
                  <div className="flex items-center gap-2">
                    <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                      {p.author_avatar_url
                        ? <img src={p.author_avatar_url} alt="" className="w-full h-full object-cover" />
                        : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{p.author_display_name?.[0]}</span>}
                    </div>
                    <div>
                      <span className="text-sm font-medium">{p.author_display_name || p.author_username}</span>
                      <span className="text-xs text-agora-400 ml-1">@{p.author_username}</span>
                    </div>
                  </div>
                  <p className="text-sm text-agora-700 dark:text-agora-300 whitespace-pre-wrap">{p.content}</p>
                  <div className="flex gap-2">
                    <button onClick={() => wallApprove.mutate(p.id)} className="btn-primary text-xs py-1 px-3 flex items-center gap-1">
                      <CheckCircle size={13} /> Approve
                    </button>
                    <button onClick={() => wallReject.mutate(p.id)} className="btn-secondary text-xs py-1 px-3 flex items-center gap-1 text-red-500">
                      <XCircle size={13} /> Reject
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Approved wall posts */}
          {(wallData?.posts || []).map((p: any) => (
            <PostCard key={p.id} post={p} invalidateKey="wall" />
          ))}
          {!(wallData?.posts || []).length && (
            <div className="card p-8 text-center text-agora-400 text-sm">
              <PenLine size={28} className="mx-auto mb-2 opacity-40" />
              <p>No wall posts yet.</p>
              {status === 'accepted' && !showWallComposer && (
                <button onClick={() => setShowWallComposer(true)} className="btn-primary text-sm mt-3">
                  Write on {profile.display_name}'s wall
                </button>
              )}
            </div>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          {isSelf && (
            <div className="flex justify-end">
              <Link to="/albums" className="btn-primary text-sm flex items-center gap-1.5">
                <Images size={14} /> Manage albums
              </Link>
            </div>
          )}
          {albums.length === 0 ? (
            <div className="card p-8 text-center text-agora-400 space-y-2">
              <Images size={28} className="mx-auto opacity-40" />
              <p>{isSelf ? "You haven't created any albums yet." : `${profile.display_name} hasn't shared any albums.`}</p>
              {isSelf && <Link to="/albums" className="btn-primary text-sm inline-block mt-1">Create an album</Link>}
            </div>
          ) : (
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
              {albums.map((a: any) => (
                <Link key={a.id} to={`/albums/${a.id}`} className="card overflow-hidden group hover:shadow-md transition-shadow">
                  <div className="aspect-square bg-agora-100 dark:bg-agora-800 overflow-hidden">
                    {a.cover_url
                      ? <img src={a.cover_url} alt="" className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300" />
                      : <div className="w-full h-full flex items-center justify-center">
                          <Images size={28} className="text-agora-300 dark:text-agora-600" />
                        </div>}
                  </div>
                  <div className="p-2.5">
                    <p className="font-semibold text-sm truncate">{a.title}</p>
                    <div className="flex items-center gap-1 text-xs text-agora-400 mt-0.5">
                      {visIcon[a.visibility]}
                      <span>{a.photo_count} photo{a.photo_count !== 1 ? 's' : ''}</span>
                    </div>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>
      )}
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
