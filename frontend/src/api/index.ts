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
  likePost:      (id: string)              => api.post(`/posts/${id}/like`),
  unlikePost:    (id: string)              => api.delete(`/posts/${id}/like`),
  repost:        (id: string, data?: any)  => api.post(`/posts/${id}/repost`, data || {}),
  getComments:   (id: string)              => api.get(`/posts/${id}/comments`),
  createComment: (id: string, data: any)  => api.post(`/posts/${id}/comments`, data),
  deleteComment: (postId: string, commentId: string) => api.delete(`/posts/${postId}/comments/${commentId}`),
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

// ── Search ────────────────────────────────────────────────────────────────────
export const searchApi = {
  searchUsers: (q: string, scope = 'local') => api.get('/search/users', { params: { q, scope } }),
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
  blockInstance:   (id: string)                => api.post(`/admin/federation/instances/${id}/block`),
  unblockInstance:        (id: string)          => api.post(`/admin/federation/instances/${id}/unblock`),
  resendVerification:     (id: string)          => api.post(`/admin/users/${id}/resend-verification`),
}


// ── Instance ──────────────────────────────────────────────────────────────────
export const instanceApi = {
  getInfo: () => api.get('/instance'),
}

