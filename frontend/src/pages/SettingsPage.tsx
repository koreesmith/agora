import { useState, useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuthStore } from '../store/auth'
import { usersApi, authApi, notificationsApi, blocksApi } from '../api'
import CoverPhoto from '../components/common/CoverPhoto'

export default function SettingsPage() {
  const { user, updateUser, logout } = useAuthStore()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'profile'|'account'|'privacy'|'notifications'|'data'|'blocked'>('profile')

  const [profile, setProfile] = useState({ display_name: user?.display_name||'', pronouns: user?.pronouns||'', bio: user?.bio||'', location: user?.location||'', website: user?.website||'' })
  const [passwords, setPasswords] = useState({ current_password:'', new_password:'' })
  const [emailChange, setEmailChange] = useState({ new_email:'', current_password:'' })
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  // Fetch fresh profile data from server to populate form with up-to-date values
  const { data: freshProfile } = useQuery({
    queryKey: ['my-profile'],
    queryFn: () => authApi.me().then(r => r.data),
  })
  useEffect(() => {
    if (freshProfile) {
      setProfile({
        display_name: freshProfile.display_name || '',
        pronouns:     freshProfile.pronouns     || '',
        bio:          freshProfile.bio           || '',
        location:     freshProfile.location      || '',
        website:      freshProfile.website       || '',
      })
      // Also sync any fields the store might be missing
      updateUser(freshProfile)
    }
  }, [freshProfile])

  const ok = (m: string) => { setMsg(m); setErr(''); setTimeout(() => setMsg(''), 3000) }
  const fail = (e: any) => setErr(e.response?.data?.error || 'Error')

  const saveProfile = useMutation({
    mutationFn: () => usersApi.updateProfile(profile),
    onSuccess: () => { updateUser(profile); ok('Profile saved') },
    onError: fail,
  })

  const savePassword = useMutation({
    mutationFn: () => authApi.changePassword(passwords),
    onSuccess: () => { setPasswords({ current_password:'', new_password:'' }); ok('Password changed') },
    onError: fail,
  })

  const changeEmail = useMutation({
    mutationFn: () => authApi.requestEmailChange(emailChange),
    onSuccess: () => { setEmailChange({ new_email:'', current_password:'' }); ok('Verification email sent — check your new inbox to confirm the change') },
    onError: fail,
  })

  const togglePrivacy = useMutation({
    mutationFn: () => {
      const newVal = !user?.profile_private
      return usersApi.updateProfile({ profile_private: newVal }).then(() => newVal)
    },
    onSuccess: (newVal) => { updateUser({ profile_private: newVal }); ok('Privacy updated') },
    onError: fail,
  })

  const toggleHideTimeline = useMutation({
    mutationFn: () => {
      const newVal = !(user as any)?.hide_timeline
      return usersApi.updateProfile({ hide_timeline: newVal } as any).then(() => newVal)
    },
    onSuccess: (newVal) => { updateUser({ hide_timeline: newVal } as any); ok('Timeline setting updated') },
    onError: fail,
  })

  const toggleWallApproval = useMutation({
    mutationFn: () => {
      const newVal = !user?.wall_approval_required
      return usersApi.updateProfile({ wall_approval_required: newVal }).then(() => newVal)
    },
    onSuccess: (newVal) => { updateUser({ wall_approval_required: newVal }); ok('Wall setting updated') },
    onError: fail,
  })

  const toggleActivityPub = useMutation({
    mutationFn: () => {
      const newVal = !user?.activitypub_enabled
      return usersApi.updateProfile({ activitypub_enabled: newVal }).then(() => newVal)
    },
    onSuccess: (newVal) => { updateUser({ activitypub_enabled: newVal }); ok('Fediverse setting updated') },
    onError: fail,
  })

  const uploadAvatar = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]; if (!f) return
    const res = await usersApi.uploadAvatar(f)
    updateUser({ avatar_url: res.data.avatar_url }); ok('Avatar updated')
  }

  const uploadCover = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]; if (!f) return
    try {
      const res = await usersApi.uploadCover(f)
      updateUser({ cover_url: res.data.cover_url }); ok('Cover photo updated')
    } catch (err: any) {
      alert(err?.response?.data?.error || 'Upload failed')
    }
  }

  const saveCoverPosition = async (pos: string) => {
    await usersApi.updateProfile({ cover_position: pos })
    updateUser({ cover_position: pos })
    ok('Cover position saved')
  }

  const exportData = async () => {
    const res = await usersApi.exportData()
    const url = URL.createObjectURL(res.data)
    const a = document.createElement('a'); a.href = url; a.download = 'agora-export.zip'; a.click()
  }

  const requestDelete = useMutation({
    mutationFn: () => usersApi.requestDeletion(),
    onSuccess: () => ok('Deletion scheduled. You have 30 days to cancel.'),
    onError: fail,
  })

  // ── Notification preferences ────────────────────────────────────────────────
  const { data: emailPrefs } = useQuery({
    queryKey: ['email-prefs'],
    queryFn: () => notificationsApi.getEmailPrefs().then(r => r.data),
    enabled: tab === 'notifications',
  })

  const [emailNotifsEnabled, setEmailNotifsEnabled] = useState(true)
  useEffect(() => {
    if (emailPrefs !== undefined) {
      setEmailNotifsEnabled(emailPrefs.email_notifications_enabled)
    }
  }, [emailPrefs])

  const toggleEmailNotifs = useMutation({
    mutationFn: (enabled: boolean) => notificationsApi.updateEmailPrefs(enabled),
    onSuccess: (_, enabled) => {
      setEmailNotifsEnabled(enabled)
      qc.invalidateQueries({ queryKey: ['email-prefs'] })
      ok(enabled ? 'Email notifications enabled' : 'Email notifications disabled')
    },
    onError: fail,
  })

  const { data: blockedData, refetch: refetchBlocked } = useQuery({
    queryKey: ['blocks'],
    queryFn: () => blocksApi.list().then(r => r.data),
    enabled: tab === 'blocked',
  })

  const unblock = useMutation({
    mutationFn: (username: string) => blocksApi.unblock(username),
    onSuccess: () => refetchBlocked(),
  })

  const tabs = ['profile','account','privacy','notifications','data','blocked'] as const

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Settings</h1>
      {msg && <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 rounded-lg px-3 py-2 text-sm text-green-700 dark:text-green-400">{msg}</div>}
      {err && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">{err}</div>}

      <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1 flex-wrap">
        {tabs.map(t => (
          <button key={t} onClick={() => setTab(t)}
            className={`flex-1 py-1.5 text-sm font-medium rounded-md transition-colors capitalize ${tab===t ? 'bg-white dark:bg-agora-700 text-agora-900 dark:text-agora-100 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
            {t}
          </button>
        ))}
      </div>

      {tab === 'profile' && (
        <div className="card p-4 space-y-4">
          {/* Cover photo */}
          <div>
            <label className="label mb-1.5">Cover photo</label>
            <div className="rounded-xl overflow-hidden">
              <CoverPhoto
                src={user?.cover_url || ''}
                position={user?.cover_position || '50% 50%'}
                height="h-36"
                editable={true}
                onUpload={uploadCover}
                onPositionSave={saveCoverPosition}
                clickable={false}
              />
            </div>
          </div>

          {/* Avatar */}
          <div className="flex items-center gap-4">
            <div className="w-16 h-16 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {user?.avatar_url ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-2xl font-bold text-agora-600">{user?.username?.[0]?.toUpperCase()}</span>}
            </div>
            <label className="btn-secondary text-sm cursor-pointer">
              Change avatar
              <input type="file" accept="image/*" className="hidden" onChange={uploadAvatar} />
            </label>
          </div>
          <div><label className="label">Display name</label><input className="input" autoComplete="name" value={profile.display_name} onChange={e=>setProfile(p=>({...p,display_name:e.target.value}))} /></div>
          <div>
            <label className="label">Pronouns</label>
            <input className="input" autoComplete="off" placeholder="e.g. she/her, he/him, they/them" value={profile.pronouns} onChange={e=>setProfile(p=>({...p,pronouns:e.target.value}))} maxLength={50} />
            <p className="text-xs text-agora-400 mt-1">Displayed beside your name on your profile and posts.</p>
          </div>
          <div><label className="label">Bio</label><textarea className="input resize-none" autoComplete="off" rows={3} value={profile.bio} onChange={e=>setProfile(p=>({...p,bio:e.target.value}))} /></div>
          <div><label className="label">Location</label><input className="input" autoComplete="off" value={profile.location} onChange={e=>setProfile(p=>({...p,location:e.target.value}))} /></div>
          <div><label className="label">Website</label><input className="input" type="url" autoComplete="url" value={profile.website} onChange={e=>setProfile(p=>({...p,website:e.target.value}))} /></div>
          <button onClick={() => saveProfile.mutate()} disabled={saveProfile.isPending} className="btn-primary">{saveProfile.isPending?'Saving…':'Save profile'}</button>
        </div>
      )}

      {tab === 'account' && (
        <div className="space-y-4">
        <div className="card p-4 space-y-4">
          <h3 className="font-semibold">Change email address</h3>
          <div>
            <label className="label">Current email</label>
            <input className="input bg-agora-50 dark:bg-agora-800" readOnly value={user?.email || ''} />
          </div>
          <div><label className="label">New email address</label><input type="email" className="input" autoComplete="email" value={emailChange.new_email} onChange={e=>setEmailChange(p=>({...p,new_email:e.target.value}))} /></div>
          <div><label className="label">Current password</label><input type="password" className="input" autoComplete="current-password" value={emailChange.current_password} onChange={e=>setEmailChange(p=>({...p,current_password:e.target.value}))} /></div>
          <p className="text-xs text-agora-400">A verification link will be sent to your new address. Your email won't change until you click the link.</p>
          <button onClick={() => changeEmail.mutate()} disabled={changeEmail.isPending || !emailChange.new_email || !emailChange.current_password} className="btn-primary">{changeEmail.isPending?'Sending…':'Send verification email'}</button>
        </div>
        <div className="card p-4 space-y-4">
          <h3 className="font-semibold">Change password</h3>
          <div><label className="label">Current password</label><input type="password" className="input" autoComplete="current-password" value={passwords.current_password} onChange={e=>setPasswords(p=>({...p,current_password:e.target.value}))} /></div>
          <div><label className="label">New password</label><input type="password" className="input" autoComplete="new-password" value={passwords.new_password} onChange={e=>setPasswords(p=>({...p,new_password:e.target.value}))} /></div>
          <button onClick={() => savePassword.mutate()} disabled={savePassword.isPending} className="btn-primary">{savePassword.isPending?'Saving…':'Change password'}</button>
        </div>
        </div>
      )}

      {tab === 'privacy' && (
        <div className="card p-4 space-y-4">
          <h3 className="font-semibold">Privacy settings</h3>
          <div className="flex items-center justify-between py-2 border-b border-agora-100 dark:border-agora-700">
            <div>
              <p className="font-medium text-sm">Private profile</p>
              <p className="text-xs text-agora-400">Only friends can see your profile page and timeline</p>
            </div>
            <button onClick={() => togglePrivacy.mutate()}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${user?.profile_private ? 'bg-agora-700' : 'bg-agora-200 dark:bg-agora-700'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${user?.profile_private ? 'translate-x-5' : 'translate-x-0'}`} />
            </button>
          </div>
          <div className="flex items-center justify-between py-2 border-b border-agora-100 dark:border-agora-700">
            <div>
              <p className="font-medium text-sm">Hide timeline from profile</p>
              <p className="text-xs text-agora-400">Nobody can browse your post history on your profile — your posts still appear in friends' feeds normally</p>
            </div>
            <button onClick={() => toggleHideTimeline.mutate()}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${(user as any)?.hide_timeline ? 'bg-agora-700' : 'bg-agora-200 dark:bg-agora-700'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${(user as any)?.hide_timeline ? 'translate-x-5' : 'translate-x-0'}`} />
            </button>
          </div>
          <div className="flex items-center justify-between py-2">
            <div>
              <p className="font-medium text-sm">Approve wall posts</p>
              <p className="text-xs text-agora-400">Review posts from friends before they appear on your wall</p>
            </div>
            <button onClick={() => toggleWallApproval.mutate()}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${user?.wall_approval_required ? 'bg-agora-700' : 'bg-agora-200 dark:bg-agora-700'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${user?.wall_approval_required ? 'translate-x-5' : 'translate-x-0'}`} />
            </button>
          </div>
          <div className="flex items-center justify-between py-2 border-b border-agora-100 dark:border-agora-700">
            <div>
              <p className="font-medium text-sm">Who can message you</p>
              <p className="text-xs text-agora-400">Control who can send you direct messages</p>
            </div>
            <select
              className="input text-sm py-1 pl-2 pr-7"
              value={(user as any)?.dm_privacy || 'everyone'}
              onChange={e => usersApi.updateProfile({ dm_privacy: e.target.value }).then(() => updateUser({ dm_privacy: e.target.value } as any))}
            >
              <option value="everyone">Everyone</option>
              <option value="friends">Friends only</option>
              <option value="nobody">Nobody</option>
            </select>
          </div>
          <div className="flex items-center justify-between py-2">
            <div>
              <p className="font-medium text-sm">Fediverse (ActivityPub)</p>
              <p className="text-xs text-agora-400">Let people on Mastodon and other fediverse apps find, follow, and see your public posts. Only applies to public posts — private and friends-only posts are never federated.</p>
            </div>
            <button onClick={() => toggleActivityPub.mutate()}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${user?.activitypub_enabled ? 'bg-agora-700' : 'bg-agora-200 dark:bg-agora-700'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${user?.activitypub_enabled ? 'translate-x-5' : 'translate-x-0'}`} />
            </button>
          </div>
        </div>
      )}

      {tab === 'notifications' && (
        <div className="card p-4 space-y-4">
          <h3 className="font-semibold">Notification settings</h3>
          <div className="flex items-center justify-between py-2 border-b border-agora-100 dark:border-agora-700">
            <div>
              <p className="font-medium text-sm">Email notifications</p>
              <p className="text-xs text-agora-400">Receive emails when someone likes, comments, or sends a friend request. Account emails like verification and password reset are always sent.</p>
            </div>
            <button
              onClick={() => toggleEmailNotifs.mutate(!emailNotifsEnabled)}
              disabled={toggleEmailNotifs.isPending}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${emailNotifsEnabled ? 'bg-agora-600' : 'bg-agora-200 dark:bg-agora-700'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${emailNotifsEnabled ? 'translate-x-5' : 'translate-x-0'}`} />
            </button>
          </div>
        </div>
      )}

      {tab === 'data' && (
        <div className="space-y-4">
          <div className="card p-4 space-y-3">
            <h3 className="font-semibold">Export your data</h3>
            <p className="text-sm text-agora-500">Download a ZIP of all your posts, friends, and profile info.</p>
            <button onClick={exportData} className="btn-secondary">Export data (ZIP)</button>
          </div>
          <div className="card p-4 space-y-3 border-red-200 dark:border-red-800">
            <h3 className="font-semibold text-red-600">Delete account</h3>
            <p className="text-sm text-agora-500">Your account will be scheduled for deletion. You have 30 days to cancel before all data is permanently removed.</p>
            <button onClick={() => { if(confirm('Schedule account deletion? You will have 30 days to cancel.')) requestDelete.mutate() }} className="btn-danger">
              Schedule deletion
            </button>
          </div>
        </div>
      )}

      {tab === 'blocked' && (
        <div className="card p-4 space-y-3">
          <h3 className="font-semibold">Blocked users</h3>
          <p className="text-sm text-agora-500">Blocked users cannot see your profile, contact you, or appear in your feed.</p>
          {(blockedData?.blocked || []).length === 0 ? (
            <p className="text-sm text-agora-400 py-4 text-center">You haven't blocked anyone.</p>
          ) : (
            <div className="space-y-2 mt-2">
              {(blockedData?.blocked || []).map((u: any) => (
                <div key={u.id} className="flex items-center gap-3 py-2 border-b border-agora-100 dark:border-agora-700 last:border-0">
                  <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {u.avatar_url
                      ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-sm">{(u.display_name || u.username)[0].toUpperCase()}</span>}
                  </div>
                  <div className="flex-1 min-w-0">
                    <Link to={`/profile/${u.username}`} className="text-sm font-medium hover:underline">{u.display_name || u.username}</Link>
                    <p className="text-xs text-agora-400">@{u.username}</p>
                  </div>
                  <button
                    onClick={() => { if (confirm(`Unblock ${u.display_name || u.username}?`)) unblock.mutate(u.username) }}
                    disabled={unblock.isPending}
                    className="btn-secondary text-xs py-1 px-3"
                  >
                    Unblock
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
