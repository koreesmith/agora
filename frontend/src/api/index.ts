import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('agora_token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

api.interceptors.response.use(
  (res) => res,
  (err) => {
    // Only treat a 401 as a session expiry (and bounce to /login) when a
    // token actually existed — guests hitting public endpoints without a
    // token should see the 401 rejected quietly, not get redirected.
    if (err.response?.status === 401 && localStorage.getItem('agora_token')) {
      localStorage.removeItem('agora_token')
      localStorage.removeItem('agora_user')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export default api

// ── Auth ──────────────────────────────────────────────────────────────────────
export const authApi = {
  register:       (data: any)   => api.post('/auth/register', data),
  login:          (data: any)   => api.post('/auth/login', data),
  me:             ()            => api.get('/auth/me'),
  changePassword:      (data: any)   => api.post('/auth/change-password', data),
  requestEmailChange:  (data: any)   => api.post('/auth/request-email-change', data),
  verifyEmailChange:   (token: string) => api.get(`/auth/verify-email-change?token=${token}`),
  forgotPassword: (email: string) => api.post('/auth/forgot-password', { email }),
  resetPassword:  (data: any)   => api.post('/auth/reset-password', data),
  verifyEmail:    (token: string) => api.get(`/auth/verify-email?token=${token}`),
}

// ── Feed ──────────────────────────────────────────────────────────────────────
export const feedApi = {
  getFeed:       (params?: { page?: number, offset?: number, limit?: number, list_id?: string, custom_feed_id?: string }) => api.get('/feed', { params }),
  getPublicFeed: (params?: { offset?: number, limit?: number }) => api.get('/feed/public', { params }),
  createPost:    (data: any)               => api.post('/posts', data),
  getPost:       (id: string)              => api.get(`/posts/${id}`),
  deletePost:    (id: string)              => api.delete(`/posts/${id}`),
  editPost:      (id: string, data: { content?: string, image_url?: string, visibility?: string, friend_list_id?: string, content_warning?: string }) => api.patch(`/posts/${id}`, data),
  likePost:      (id: string)              => api.post(`/posts/${id}/like`),
  unlikePost:    (id: string)              => api.delete(`/posts/${id}/like`),
  reactPost:     (id: string, type: string) => api.post(`/posts/${id}/react`, { type }),
  unreactPost:   (id: string)              => api.delete(`/posts/${id}/react`),
  getReactions:  (id: string)              => api.get(`/posts/${id}/reactions`),
  repost:        (id: string, data?: any)  => api.post(`/posts/${id}/repost`, data || {}),
  getComments:   (id: string)              => api.get(`/posts/${id}/comments`),
  createComment: (id: string, data: { content: string, image_url?: string, reply_to_id?: string }) => api.post(`/posts/${id}/comments`, data),
  deleteComment: (postId: string, commentId: string) => api.delete(`/posts/${postId}/comments/${commentId}`),
  editComment:   (postId: string, commentId: string, content: string) => api.patch(`/posts/${postId}/comments/${commentId}`, { content }),
  pollVote:      (id: string, option_id: string) => api.post(`/posts/${id}/poll/vote`, { option_id }),
  pollUnvote:    (id: string)              => api.delete(`/posts/${id}/poll/vote`),
  pollAddOption: (id: string, text: string) => api.post(`/posts/${id}/poll/options`, { text }),
  getPollVoters:      (id: string)              => api.get(`/posts/${id}/poll/voters`),
  groupMentionSearch: (q: string)               => api.get('/groups/mention-search', { params: { q } }),
  getWall:       (username: string)        => api.get(`/users/${username}/wall`),
  getWallQueue:  ()                        => api.get('/users/me/wall-queue'),
  wallApprove:   (id: string)              => api.post(`/posts/${id}/wall-approve`),
  wallReject:    (id: string)              => api.post(`/posts/${id}/wall-reject`),
  getUserPosts:  (username: string, params?: any) => api.get(`/users/${username}/posts`, { params }),
  uploadMedia:   (file: File, category = 'posts') => {
    const form = new FormData()
    form.append('file', file)
    return api.post(`/media/upload?category=${category}`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  // AGORA-137: poll async video transcode job status
  getVideoJob:   (jobId: string) => api.get(`/media/jobs/${jobId}`),
}

// ── Users ─────────────────────────────────────────────────────────────────────
export const usersApi = {
  getProfile:       (username: string) => api.get(`/users/${username}`),
  updateProfile:    (data: any)        => api.patch('/users/me', data),
  uploadAvatar:     (file: File)       => {
    const form = new FormData(); form.append('file', file)
    return api.post('/users/me/avatar', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  uploadCover:      (file: File)       => {
    const form = new FormData(); form.append('file', file)
    return api.post('/users/me/cover', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  exportData:       ()                 => api.get('/users/me/export', { responseType: 'blob' }),
  requestDeletion:  ()                 => api.post('/users/me/request-deletion'),
  cancelDeletion:   ()                 => api.delete('/users/me/request-deletion'),
  deleteImmediately:()                 => api.post('/users/me/delete-immediately'),
  discover:         ()                 => api.get('/users/discover'),
  mentionSearch:    (q: string)        => api.get('/users/mention-search', { params: { q } }),
  enablePostNotify: (username: string) => api.post(`/users/${username}/notify`),
  disablePostNotify:(username: string) => api.delete(`/users/${username}/notify`),
}

// ── Friends ───────────────────────────────────────────────────────────────────
export const friendsApi = {
  listFriends:      ()                           => api.get('/friends'),
  listRequests:     ()                           => api.get('/friends/requests'),
  sendRequest:      (userID: string)             => api.post(`/friends/request/${userID}`),
  acceptRequest:    (userID: string)             => api.post(`/friends/accept/${userID}`),
  declineRequest:   (userID: string)             => api.post(`/friends/decline/${userID}`),
  unfriend:         (userID: string)             => api.delete(`/friends/${userID}`),
  listFriendLists:    ()                           => api.get('/friend-groups'),
  createFriendList:   (name: string)               => api.post('/friend-groups', { name }),
  deleteFriendList:   (id: string)                 => api.delete(`/friend-groups/${id}`),
  listFriendListMembers: (listID: string)          => api.get(`/friend-groups/${listID}/members`),
  addToFriendList:    (listID: string, friendID: string) => api.post(`/friend-groups/${listID}/members/${friendID}`),
  removeFromFriendList: (listID: string, friendID: string) => api.delete(`/friend-groups/${listID}/members/${friendID}`),
}

// ── Notifications ─────────────────────────────────────────────────────────────
export const notificationsApi = {
  list:            (params?: any) => api.get('/notifications', { params }),
  unreadCount:     ()             => api.get('/notifications/unread-count'),
  markAllRead:     ()             => api.post('/notifications/read-all'),
  markRead:        (id: string)   => api.post(`/notifications/${id}/read`),
  markManyRead:    (ids: string[]) => api.post('/notifications/read-many', { ids }),
  getEmailPrefs:   ()             => api.get('/notifications/email-preferences'),
  updateEmailPrefs:(enabled: boolean) => api.put('/notifications/email-preferences', { email_notifications_enabled: enabled }),
}

// ── Groups ────────────────────────────────────────────────────────────────────
export const groupsApi = {
  list:           (params?: { q?: string, filter?: string, page?: number }) => api.get('/groups', { params }),
  get:            (slug: string)                              => api.get(`/groups/${slug}`),
  create:         (data: { name: string, description: string, privacy: string }) => api.post('/groups', data),
  update:         (slug: string, data: any)                  => api.patch(`/groups/${slug}`, data),
  delete:         (slug: string)                             => api.delete(`/groups/${slug}`),
  listMembers:    (slug: string)                             => api.get(`/groups/${slug}/members`),
  join:           (slug: string)                             => api.post(`/groups/${slug}/join`),
  leave:          (slug: string)                             => api.delete(`/groups/${slug}/leave`),
  setRole:        (slug: string, userID: string, role: string) => api.patch(`/groups/${slug}/members/${userID}/role`, { role }),
  removeMember:   (slug: string, userID: string)             => api.delete(`/groups/${slug}/members/${userID}`),
  addMember:      (slug: string, username: string)           => api.post(`/groups/${slug}/members/add`, { username }),
  memberSearch:   (slug: string, q: string)                  => api.get(`/groups/${slug}/member-search`, { params: { q } }),
  getFeed:        (slug: string, page = 0)                   => api.get(`/groups/${slug}/feed`, { params: { page } }),
  createPost:     (slug: string, data: { content: string, image_url?: string, poll_options?: string[] }) => api.post(`/groups/${slug}/posts`, data),
  // Invite links
  listInvites:    (slug: string)                             => api.get(`/groups/${slug}/invites`),
  createInvite:   (slug: string, maxUses = 0)                => api.post(`/groups/${slug}/invites`, { max_uses: maxUses }),
  revokeInvite:   (slug: string, token: string)              => api.delete(`/groups/${slug}/invites/${token}`),
  joinByInvite:   (token: string)                            => api.get(`/groups/join-by-invite/${token}`),
  // Join requests
  requestJoin:    (slug: string, message = '')               => api.post(`/groups/${slug}/request`, { message }),
  listRequests:   (slug: string)                             => api.get(`/groups/${slug}/requests`),
  approveRequest: (slug: string, requestID: string)          => api.post(`/groups/${slug}/requests/${requestID}/approve`),
  rejectRequest:  (slug: string, requestID: string)          => api.post(`/groups/${slug}/requests/${requestID}/reject`),
}

// ── Interactions (AGORA-102) ──────────────────────────────────────────────────
export const interactionsApi = {
  record: (data: { target_user_id?: string, post_id?: string, interaction_type: string }) =>
    api.post('/feed/interactions', data).catch(() => {}), // fire-and-forget
  reset: () => api.delete('/feed/interactions'),          // AGORA-104: clear history
}


// ── Pages (AGORA-106) ─────────────────────────────────────────────────────────
// ── Page Members (AGORA-112) ──────────────────────────────────────────────────
export const pageMembersApi = {
  list:       (slug: string)                                      => api.get(`/pages/${slug}/members`),
  invite:     (slug: string, username: string, role: string)      => api.post(`/pages/${slug}/members`, { username, role }),
  accept:     (slug: string)                                      => api.post(`/pages/${slug}/members/accept`),
  setRole:    (slug: string, userId: string, role: string)        => api.patch(`/pages/${slug}/members/${userId}/role`, { role }),
  remove:     (slug: string, userId: string)                      => api.delete(`/pages/${slug}/members/${userId}`),
}

export const pagesApi = {
  list:        (params?: { q?: string, page?: number, featured?: boolean }) => api.get('/pages', { params }),
  featured:    ()                                        => api.get('/pages', { params: { featured: true } }),
  mine:        ()                                        => api.get('/pages/mine'),
  get:         (slug: string)                            => api.get(`/pages/${slug}`),
  create:      (data: { display_name: string, bio?: string, page_type?: string, privacy?: string, avatar_url?: string, cover_url?: string }) => api.post('/pages', data),
  update:      (slug: string, data: any)                 => api.patch(`/pages/${slug}`, data),
  delete:      (slug: string)                            => api.delete(`/pages/${slug}`),
  subscribe:   (slug: string)                            => api.post(`/pages/${slug}/subscribe`),
  unsubscribe: (slug: string)                            => api.delete(`/pages/${slug}/subscribe`),
  getFeed:     (slug: string, page = 0)                  => api.get(`/pages/${slug}/feed`, { params: { page } }),
  createPost:  (slug: string, data: { content: string, image_url?: string, image_urls?: string[] }) => api.post(`/pages/${slug}/posts`, data),
  analytics:   (slug: string)                            => api.get(`/pages/${slug}/analytics`),
}

// ── Search ────────────────────────────────────────────────────────────────────
export const searchApi = {
  searchUsers: (q: string, scope = 'local') => api.get('/search/users', { params: { q, scope } }),
  searchPosts: (q: string, page = 0)        => api.get('/search/posts', { params: { q, page } }),
  searchPages: (q: string, page = 0)        => api.get('/search/pages', { params: { q, page } }),
}

// ── Moderation ────────────────────────────────────────────────────────────────
export const moderationApi = {
  createReport:       (data: any)                 => api.post('/reports', data),
  listReports:        (status?: string)           => api.get('/moderation/reports', { params: { status } }),
  reviewReport:       (id: string, data: any)     => api.post(`/moderation/reports/${id}/review`, data),
  listModeratedUsers: (filter?: string)           => api.get('/moderation/users', { params: { filter } }),
  suspendUser:        (id: string, data: any)     => api.post(`/moderation/users/${id}/suspend`, data),
  unsuspendUser:      (id: string)                => api.post(`/moderation/users/${id}/unsuspend`, {}),
  banUser:            (id: string, data: any)     => api.post(`/moderation/users/${id}/ban`, data),
  unbanUser:          (id: string)                => api.post(`/moderation/users/${id}/unban`, {}),
  listInstanceBans:   ()                          => api.get('/moderation/instance-bans'),
  banInstance:        (data: any)                 => api.post('/moderation/instance-bans', data),
  unbanInstance:      (id: string)                => api.delete(`/moderation/instance-bans/${id}`),
  // AGORA-205: DID-scoped block list, the AT Proto counterpart to instance-bans.
  listBlockedDIDs:    ()                          => api.get('/moderation/blocked-dids'),
  blockDID:           (data: any)                 => api.post('/moderation/blocked-dids', data),
  unblockDID:         (id: string)                => api.delete(`/moderation/blocked-dids/${id}`),
}

// ── Admin ─────────────────────────────────────────────────────────────────────
// ── Admin Pages (AGORA-114) ───────────────────────────────────────────────────
export const adminPagesApi = {
  verify:  (slug: string, verified: boolean)  => api.patch(`/admin/pages/${slug}/verify`,  { verified }),
  feature: (slug: string, featured: boolean)  => api.patch(`/admin/pages/${slug}/feature`, { featured }),
}

export const adminApi = {
  getSettings:     ()                          => api.get('/admin/settings'),
  updateSettings:  (data: any)                 => api.patch('/admin/settings', data),
  getStats:        ()                          => api.get('/admin/stats'),
  listUsers:       (q?: string)                => api.get('/admin/users', { params: { q } }),
  setRole:         (userID: string, role: string) => api.patch(`/admin/users/${userID}/role`, { role }),
  deleteUser:      (userID: string)            => api.delete(`/admin/users/${userID}`),
  listInvites:     ()                          => api.get('/admin/invites'),
  createInvite:    ()                          => api.post('/admin/invites'),
  revokeInvite:    (id: string)                => api.delete(`/admin/invites/${id}`),
  getAuditLog:     ()                          => api.get('/admin/audit-log'),
  listInstances:   ()                          => api.get('/admin/federation/instances'),
  addInstance:     (domain: string)            => api.post('/admin/federation/instances', { domain }),
  blockInstance:   (id: string)                => api.post(`/admin/federation/instances/${id}/block`),
  unblockInstance: (id: string)                => api.post(`/admin/federation/instances/${id}/unblock`),
  resendVerification:     (id: string)          => api.post(`/admin/users/${id}/resend-verification`),
  // Instance rules
  listRules:   ()                               => api.get('/admin/rules'),
  createRule:  (text: string)                   => api.post('/admin/rules', { text }),
  updateRule:  (id: string, text: string)       => api.patch(`/admin/rules/${id}`, { text }),
  deleteRule:  (id: string)                     => api.delete(`/admin/rules/${id}`),
  moveRule:    (id: string, direction: 'up'|'down') => api.patch(`/admin/rules/${id}/move`, { direction }),
  // Waitlist
  listWaitlist:    ()           => api.get('/admin/waitlist'),
  approveWaitlist: (id: string) => api.post(`/admin/waitlist/${id}/approve`),
  rejectWaitlist:  (id: string) => api.delete(`/admin/waitlist/${id}`),
  // Media cleanup
  scanOrphans:   () => api.get('/admin/media/orphans'),
  deleteOrphans: () => api.delete('/admin/media/orphans'),
  // Fediverse relays (AGORA-223)
  listRelays:   ()               => api.get('/admin/relays'),
  addRelay:     (inboxUrl: string) => api.post('/admin/relays', { inbox_url: inboxUrl }),
  enableRelay:  (id: string)     => api.post(`/admin/relays/${id}/enable`),
  disableRelay: (id: string)     => api.post(`/admin/relays/${id}/disable`),
  deleteRelay:  (id: string)     => api.delete(`/admin/relays/${id}`),
}


// ── Link preview ──────────────────────────────────────────────────────────────
export const previewApi = {
  fetch: (url: string) => api.get('/preview', { params: { url } }),
}

// ── Federation ────────────────────────────────────────────────────────────────
export const federationApi = {
  lookupUser: (handle: string) => api.get('/federation/lookup', { params: { handle } }),
  // AGORA-146: resolve a fediverse handle/URL to a preview (search), follow/
  // unfollow a remote account, and list current follows.
  resolveFediverseHandle:   (handle: string)   => api.get('/federation/ap-lookup', { params: { handle } }),
  followFediverseAccount:   (actorUrl: string) => api.post('/federation/follow', { actor_url: actorUrl }),
  unfollowFediverseAccount: (id: string)       => api.delete(`/federation/follow/${id}`),
  listFollowing:            ()                 => api.get('/federation/following'),
  toggleFollowNotify:       (id: string, notify: boolean) => api.put(`/federation/follow/${id}/notify`, { notify }),
  toggleShowInFeed:         (id: string, showInFeed: boolean) => api.put(`/federation/follow/${id}/show-in-feed`, { show_in_feed: showInFeed }),
}

// ── AT Proto / Bluesky ───────────────────────────────────────────────────────
export const atprotoApi = {
  // AGORA-195: resolve a Bluesky handle/DID to a live preview (search),
  // follow/unfollow a native Bluesky account, and list current follows —
  // the AT Proto counterpart to federationApi's fediverse equivalents.
  resolveBlueskyHandle:   (handle: string) => api.get('/atproto/lookup', { params: { handle } }),
  followBlueskyAccount:   (actor: string)  => api.post('/atproto/follow', { actor }),
  unfollowBlueskyAccount: (id: string)     => api.delete(`/atproto/follow/${id}`),
  listBlueskyFollowing:   ()               => api.get('/atproto/following'),
  // AGORA-198: per-follow notification opt-in, mirroring toggleFollowNotify.
  toggleFollowNotify:     (id: string, notify: boolean) => api.put(`/atproto/follow/${id}/notify`, { notify }),
  // AGORA-196: reconcile a Bridgy-Fed-bridged Bluesky follow (an ap_following
  // row) into a native one.
  migrateBridgedFollow:   (apFollowingId: string) => api.post(`/atproto/bridged-follows/${apFollowingId}/migrate`),
}

// ── Albums ────────────────────────────────────────────────────────────────────
export const albumsApi = {
  list:         (page = 0)                     => api.get('/albums', { params: { page } }),
  listForUser:  (username: string)             => api.get(`/users/${username}/albums`),
  get:          (id: string)                   => api.get(`/albums/${id}`),
  create:       (data: { title: string, description?: string, visibility?: string, friend_group_id?: string }) => api.post('/albums', data),
  update:       (id: string, data: any)        => api.patch(`/albums/${id}`, data),
  delete:       (id: string)                   => api.delete(`/albums/${id}`),
  addPhoto:     (id: string, data: { url: string, caption?: string }) => api.post(`/albums/${id}/photos`, data),
  uploadPhoto:  (id: string, file: File, caption = '') => {
    const form = new FormData()
    form.append('file', file)
    form.append('caption', caption)
    return api.post(`/albums/${id}/photos`, form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  updatePhoto:  (albumId: string, photoId: string, data: { caption?: string, position?: number }) =>
    api.patch(`/albums/${albumId}/photos/${photoId}`, data),
  deletePhoto:  (albumId: string, photoId: string) => api.delete(`/albums/${albumId}/photos/${photoId}`),
}

// ── Instance ──────────────────────────────────────────────────────────────────
export const instanceApi = {
  getInfo:    () => api.get('/instance'),
  getRules:   () => api.get('/instance/rules'),
  uploadLogo: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/media/upload?category=instance', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
}

// ── Blocks ────────────────────────────────────────────────────────────────────
export const blocksApi = {
  list:     ()                 => api.get('/blocks'),
  block:    (username: string) => api.post(`/blocks/${username}`),
  unblock:  (username: string) => api.delete(`/blocks/${username}`),
}
export const dmApi = {
  listConversations:  ()                                     => api.get('/conversations'),
  startConversation:  (username: string, message?: string)  => api.post('/conversations', { username, message }),
  friendSearch:       (q: string)                           => api.get('/conversations/friend-search', { params: { q } }),
  getConversation:    (id: string)                          => api.get(`/conversations/${id}`),
  getMessages:        (id: string, before?: string)         => api.get(`/conversations/${id}/messages`, { params: before ? { before } : {} }),
  sendMessage:        (id: string, content: string, image_url?: string) => api.post(`/conversations/${id}/messages`, { content, image_url }),
  editMessage:        (id: string, content: string)         => api.patch(`/messages/${id}`, { content }),
  deleteMessage:      (id: string)                          => api.delete(`/messages/${id}`),
  reactMessage:       (id: string, reaction: string)        => api.post(`/messages/${id}/react`, { reaction }),
  unreactMessage:     (id: string)                          => api.delete(`/messages/${id}/react`),
  markRead:           (id: string)                          => api.post(`/conversations/${id}/read`),
  acceptRequest:      (id: string)                          => api.post(`/conversations/${id}/accept`),
  leaveConversation:  (id: string)                          => api.delete(`/conversations/${id}`),
}

export const inviteApi = {
  send: (email: string) => api.post('/invites/send', { email }),
}

// ── Custom Feeds ──────────────────────────────────────────────────────────────
export const customFeedsApi = {
  list:   ()                                                                                                         => api.get('/feeds'),
  get:    (id: string)                                                                                               => api.get(`/feeds/${id}`),
  create: (data: { name: string, smart_ranking?: boolean, filters: { filter_type: string, value: string }[] })      => api.post('/feeds', data),
  update: (id: string, data: { name: string, smart_ranking?: boolean, filters: { filter_type: string, value: string }[] }) => api.put(`/feeds/${id}`, data),
  delete: (id: string)                                                                                               => api.delete(`/feeds/${id}`),
}
