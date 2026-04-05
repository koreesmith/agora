# Frontend API Client

**File:** `frontend/src/api/index.ts`

Typed Axios client. All API calls go through this module.

## Configuration

```typescript
import axios from 'axios'

const api = axios.create({ baseURL: '/api' })

// Request interceptor: attach JWT from localStorage
api.interceptors.request.use(config => {
  const token = localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

// Response interceptor: 401 → clear auth and redirect to /login
api.interceptors.response.use(
  res => res,
  err => {
    if (err.response?.status === 401) {
      localStorage.clear()
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)
```

## API Groups

### `authApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `register` | `(data: RegisterData)` | POST /auth/register |
| `login` | `(data: LoginData)` | POST /auth/login |
| `me` | `()` | GET /auth/me |
| `changePassword` | `(data)` | POST /auth/change-password |
| `requestEmailChange` | `(data)` | POST /auth/request-email-change |
| `verifyEmailChange` | `(token: string)` | GET /auth/verify-email-change |
| `forgotPassword` | `(email: string)` | POST /auth/forgot-password |
| `resetPassword` | `(data)` | POST /auth/reset-password |
| `verifyEmail` | `(token: string)` | GET /auth/verify-email |

### `feedApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `getFeed` | `(params: {page?, list_id?})` | GET /feed |
| `createPost` | `(data: CreatePostData)` | POST /posts |
| `getPost` | `(id: string)` | GET /posts/{id} |
| `deletePost` | `(id: string)` | DELETE /posts/{id} |
| `editPost` | `(id: string, data)` | PATCH /posts/{id} |
| `likePost` | `(id: string)` | POST /posts/{id}/like |
| `unlikePost` | `(id: string)` | DELETE /posts/{id}/like |
| `reactPost` | `(id: string, type: string)` | POST /posts/{id}/react |
| `unreactPost` | `(id: string)` | DELETE /posts/{id}/react |
| `getReactions` | `(id: string)` | GET /posts/{id}/reactions |
| `repost` | `(id: string, data)` | POST /posts/{id}/repost |
| `getComments` | `(id: string)` | GET /posts/{id}/comments |
| `createComment` | `(id: string, data)` | POST /posts/{id}/comments |
| `deleteComment` | `(postId: string, commentId: string)` | DELETE /posts/{postId}/comments/{commentId} |
| `editComment` | `(postId, commentId, content)` | PATCH /posts/{postId}/comments/{commentId} |
| `pollVote` | `(id: string, option_id: string)` | POST /posts/{id}/poll/vote |
| `pollUnvote` | `(id: string)` | DELETE /posts/{id}/poll/vote |
| `pollAddOption` | `(id: string, text: string)` | POST /posts/{id}/poll/options |
| `getWall` | `(username: string)` | GET /users/{username}/wall |
| `getWallQueue` | `()` | GET /users/me/wall-queue |
| `wallApprove` | `(id: string)` | POST /posts/{id}/wall-approve |
| `wallReject` | `(id: string)` | POST /posts/{id}/wall-reject |
| `getUserPosts` | `(username: string, params?)` | GET /users/{username}/posts |
| `uploadMedia` | `(file: File, category: string)` | POST /media/upload |
| `getLinkPreview` | `(url: string)` | GET /preview |

### `usersApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `getProfile` | `(username: string)` | GET /users/{username} |
| `updateProfile` | `(data: Partial<Profile>)` | PATCH /users/me |
| `uploadAvatar` | `(file: File)` | POST /users/me/avatar |
| `uploadCover` | `(file: File)` | POST /users/me/cover |
| `exportData` | `()` | GET /users/me/export |
| `requestDeletion` | `()` | POST /users/me/request-deletion |
| `cancelDeletion` | `()` | DELETE /users/me/request-deletion |
| `deleteImmediately` | `()` | POST /users/me/delete-immediately |
| `discover` | `()` | GET /users/discover |
| `mentionSearch` | `(q: string)` | GET /users/mention-search |
| `enablePostNotify` | `(username: string)` | POST /users/{username}/notify |
| `disablePostNotify` | `(username: string)` | DELETE /users/{username}/notify |

### `friendsApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `listFriends` | `()` | GET /friends |
| `listRequests` | `()` | GET /friends/requests |
| `sendRequest` | `(userID: string)` | POST /friends/request/{userID} |
| `acceptRequest` | `(userID: string)` | POST /friends/accept/{userID} |
| `declineRequest` | `(userID: string)` | POST /friends/decline/{userID} |
| `unfriend` | `(userID: string)` | DELETE /friends/{userID} |
| `listFriendLists` | `()` | GET /friend-groups |
| `createFriendList` | `(name: string)` | POST /friend-groups |
| `deleteFriendList` | `(id: string)` | DELETE /friend-groups/{id} |
| `listFriendListMembers` | `(listID: string)` | GET /friend-groups/{listID}/members |
| `addToFriendList` | `(listID, friendID)` | POST /friend-groups/{listID}/members/{friendID} |
| `removeFromFriendList` | `(listID, friendID)` | DELETE /friend-groups/{listID}/members/{friendID} |

### `notificationsApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `list` | `(params?)` | GET /notifications |
| `unreadCount` | `()` | GET /notifications/unread-count |
| `markAllRead` | `()` | POST /notifications/read-all |
| `markRead` | `(id: string)` | POST /notifications/{id}/read |
| `getEmailPrefs` | `()` | GET /notifications/email-preferences |
| `updateEmailPrefs` | `(enabled: boolean)` | PUT /notifications/email-preferences |

### `groupsApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `list` | `(params?)` | GET /groups |
| `get` | `(slug: string)` | GET /groups/{slug} |
| `create` | `(data)` | POST /groups |
| `update` | `(slug, data)` | PATCH /groups/{slug} |
| `delete` | `(slug: string)` | DELETE /groups/{slug} |
| `listMembers` | `(slug: string)` | GET /groups/{slug}/members |
| `join` | `(slug: string)` | POST /groups/{slug}/join |
| `leave` | `(slug: string)` | DELETE /groups/{slug}/leave |
| `setRole` | `(slug, userID, role)` | PATCH /groups/{slug}/members/{userID}/role |
| `removeMember` | `(slug, userID)` | DELETE /groups/{slug}/members/{userID} |
| `addMember` | `(slug, username)` | POST /groups/{slug}/members/add |
| `memberSearch` | `(slug, q)` | GET /groups/{slug}/member-search |
| `getFeed` | `(slug, page?)` | GET /groups/{slug}/feed |
| `createPost` | `(slug, data)` | POST /groups/{slug}/posts |
| `listInvites` | `(slug: string)` | GET /groups/{slug}/invites |
| `createInvite` | `(slug, maxUses?)` | POST /groups/{slug}/invites |
| `revokeInvite` | `(slug, token)` | DELETE /groups/{slug}/invites/{token} |
| `requestJoin` | `(slug, message?)` | POST /groups/{slug}/request |
| `listRequests` | `(slug: string)` | GET /groups/{slug}/requests |
| `approveRequest` | `(slug, requestID)` | POST /groups/{slug}/requests/{requestID}/approve |
| `rejectRequest` | `(slug, requestID)` | POST /groups/{slug}/requests/{requestID}/reject |

### `searchApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `searchUsers` | `(q: string, scope?: string)` | GET /search/users |
| `searchPosts` | `(q: string, page?: number)` | GET /search/posts |

### `moderationApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `createReport` | `(data)` | POST /reports |
| `listReports` | `(status?: string)` | GET /moderation/reports |
| `reviewReport` | `(id, data)` | POST /moderation/reports/{id}/review |
| `listModeratedUsers` | `(filter?)` | GET /moderation/users |
| `suspendUser` | `(id, data)` | POST /moderation/users/{id}/suspend |
| `unsuspendUser` | `(id: string)` | POST /moderation/users/{id}/unsuspend |
| `banUser` | `(id, data)` | POST /moderation/users/{id}/ban |
| `unbanUser` | `(id: string)` | POST /moderation/users/{id}/unban |
| `listInstanceBans` | `()` | GET /moderation/instance-bans |
| `banInstance` | `(data)` | POST /moderation/instance-bans |
| `unbanInstance` | `(id: string)` | DELETE /moderation/instance-bans/{id} |

### `adminApi`

| Method | Signature | HTTP |
|--------|-----------|------|
| `getSettings` | `()` | GET /admin/settings |
| `updateSettings` | `(data)` | PATCH /admin/settings |
| `getStats` | `()` | GET /admin/stats |
| `listUsers` | `(q?)` | GET /admin/users |
| `setRole` | `(userID, role)` | PATCH /admin/users/{userID}/role |
| `deleteUser` | `(userID: string)` | DELETE /admin/users/{userID} |
| `listInvites` | `()` | GET /admin/invites |
| `createInvite` | `()` | POST /admin/invites |
| `revokeInvite` | `(id: string)` | DELETE /admin/invites/{id} |
| `getAuditLog` | `()` | GET /admin/audit-log |
| `listInstances` | `()` | GET /admin/federation/instances |
| `addInstance` | `(domain: string)` | POST /admin/federation/instances |
| `blockInstance` | `(id: string)` | POST /admin/federation/instances/{id}/block |
| `unblockInstance` | `(id: string)` | POST /admin/federation/instances/{id}/unblock |
| `resendVerification` | `(id: string)` | POST /admin/users/{id}/resend-verification |
| `listRules` | `()` | GET /admin/rules |
| `createRule` | `(text: string)` | POST /admin/rules |
| `updateRule` | `(id, text)` | PATCH /admin/rules/{id} |
| `deleteRule` | `(id: string)` | DELETE /admin/rules/{id} |
| `moveRule` | `(id, direction)` | PATCH /admin/rules/{id}/move |
| `listWaitlist` | `()` | GET /admin/waitlist |
| `approveWaitlist` | `(id: string)` | POST /admin/waitlist/{id}/approve |
| `rejectWaitlist` | `(id: string)` | DELETE /admin/waitlist/{id} |
