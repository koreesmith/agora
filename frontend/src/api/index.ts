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
    if (err.response?.status === 401) {
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
  changePassword: (data: any)   => api.post('/auth/change-password', data),
  forgotPassword: (email: string) => api.post('/auth/forgot-password', { email }),
  resetPassword:  (data: any)   => api.post('/auth/reset-password', data),
  verifyEmail:    (token: string) => api.get(`/auth/verify-email?token=${token}`),
}

// ── Feed ──────────────────────────────────────────────────────────────────────
export const feedApi = {
  getFeed:       (params?: { page?: number, offset?: number, limit?: number, list_id?: string }) => api.get('/feed', { params }),
  createPost:    (data: any)               => api.post('/posts', data),
  getPost:       (id: string)              => api.get(`/posts/${id}`),
  deletePost:    (id: string)              => api.delete(`/posts/${id}`),
  editPost:      (id: string, data: { content?: string, image_url?: string, visibility?: string, friend_list_id?: string }) => api.patch(`/posts/${id}`, data),
  likePost:      (id: string)              => api.post(`/posts/${id}/like`),
  unlikePost:    (id: string)              => api.delete(`/posts/${id}/like`),
  repost:        (id: string, data?: any)  => api.post(`/posts/${id}/repost`, data || {}),
  getComments:   (id: string)              => api.get(`/posts/${id}/comments`),
  createComment: (id: string, data: { content: string, image_url?: string, reply_to_id?: string }) => api.post(`/posts/${id}/comments`, data),
  deleteComment: (postId: string, commentId: string) => api.delete(`/posts/${postId}/comments/${commentId}`),
  editComment:   (postId: string, commentId: string, content: string) => api.patch(`/posts/${postId}/comments/${commentId}`, { content }),
  getUserPosts:  (username: string, params?: any) => api.get(`/users/${username}/posts`, { params }),
  uploadMedia:   (file: File, category = 'posts') => {
    const form = new FormData()
    form.append('file', file)
    return api.post(`/media/upload?category=${category}`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
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
}

// ── Friends ───────────────────────────────────────────────────────────────────
export const friendsApi = {
  listFriends:      ()                           => api.get('/friends'),
  listRequests:     ()                           => api.get('/friends/requests'),
  sendRequest:      (userID: string)             => api.post(`/friends/request/${userID}`),
  acceptRequest:    (userID: string)             => api.post(`/friends/accept/${userID}`),
  declineRequest:   (userID: string)             => api.post(`/friends/decline/${userID}`),
  unfriend:         (userID: string)             => api.delete(`/friends/${userID}`),
  listGroups:       ()                           => api.get('/friend-groups'),
  createGroup:      (name: string)               => api.post('/friend-groups', { name }),
  deleteGroup:      (id: string)                 => api.delete(`/friend-groups/${id}`),
  listGroupMembers: (groupID: string)            => api.get(`/friend-groups/${groupID}/members`),
  addToGroup:       (groupID: string, friendID: string) => api.post(`/friend-groups/${groupID}/members/${friendID}`),
  removeFromGroup:  (groupID: string, friendID: string) => api.delete(`/friend-groups/${groupID}/members/${friendID}`),
}

// ── Notifications ─────────────────────────────────────────────────────────────
export const notificationsApi = {
  list:            (params?: any) => api.get('/notifications', { params }),
  unreadCount:     ()             => api.get('/notifications/unread-count'),
  markAllRead:     ()             => api.post('/notifications/read-all'),
  markRead:        (id: string)   => api.post(`/notifications/${id}/read`),
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
  createPost:     (slug: string, data: { content: string, image_url?: string }) => api.post(`/groups/${slug}/posts`, data),
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

// ── Search ────────────────────────────────────────────────────────────────────
export const searchApi = {
  searchUsers: (q: string, scope = 'local') => api.get('/search/users', { params: { q, scope } }),
  searchPosts: (q: string, page = 0)        => api.get('/search/posts', { params: { q, page } }),
}

// ── Moderation ────────────────────────────────────────────────────────────────
export const moderationApi = {
  createReport:  (data: any)               => api.post('/reports', data),
  listReports:   (status?: string)         => api.get('/moderation/reports', { params: { status } }),
  reviewReport:  (id: string, data: any)   => api.post(`/moderation/reports/${id}/review`, data),
  suspendUser:   (userID: string, reason: string) => api.post(`/moderation/users/${userID}/suspend`, { reason }),
  unsuspendUser: (userID: string)          => api.post(`/moderation/users/${userID}/unsuspend`),
}

// ── Admin ─────────────────────────────────────────────────────────────────────
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
}


// ── Federation ────────────────────────────────────────────────────────────────
export const federationApi = {
  lookupUser: (handle: string) => api.get('/federation/lookup', { params: { handle } }),
}

// ── Albums ────────────────────────────────────────────────────────────────────
export const albumsApi = {
  list:         (page = 0)                     => api.get('/albums', { params: { page } }),
  listForUser:  (username: string)             => api.get(`/users/${username}/albums`),
  get:          (id: string)                   => api.get(`/albums/${id}`),
  create:       (data: { title: string, description?: string, visibility?: string }) => api.post('/albums', data),
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
  getInfo:  () => api.get('/instance'),
  getRules: () => api.get('/instance/rules'),
}

