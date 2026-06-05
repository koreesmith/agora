import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi, moderationApi, instanceApi, adminPagesApi, pagesApi } from '../api'
import { Users, Settings, Flag, Link2, Ticket, BookOpen, List, Clock, ShieldAlert, X, Star } from 'lucide-react'

export default function AdminPage() {
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<'overview'|'settings'|'users'|'reports'|'moderation'|'federation'|'invites'|'rules'|'waitlist'|'pages'>(
    (searchParams.get('tab') as any) || 'overview'
  )
  const [settingsForm, setSettingsForm] = useState<Record<string,string>>({})
  const [msg, setMsg] = useState('')
  const [reportStatus, setReportStatus] = useState('pending')
  const [reportNotes, setReportNotes] = useState<Record<string,string>>({})
  const [suspendForm, setSuspendForm] = useState<Record<string, {days:string, reason:string, notes:string}>>({})
  const [banForm, setBanForm] = useState<Record<string, {reason:string, notes:string}>>({})
  const [instanceBanForm, setInstanceBanForm] = useState({ instance:'', reason:'', notes:'' })
  const qc = useQueryClient()
  const ok = (m:string) => { setMsg(m); setTimeout(()=>setMsg(''), 3000) }

  const { data: stats }    = useQuery({ queryKey:['admin-stats'],    queryFn: ()=>adminApi.getStats().then(r=>r.data),    enabled: tab==='overview' })
  const { data: settings } = useQuery({ queryKey:['admin-settings'], queryFn: ()=>adminApi.getSettings().then(r=>r.data), enabled: tab==='settings' })
  const { data: usersData }= useQuery({ queryKey:['admin-users'],    queryFn: ()=>adminApi.listUsers().then(r=>r.data),   enabled: tab==='users' })
  const { data: repsData } = useQuery({ queryKey:['admin-reports', reportStatus], queryFn: ()=>moderationApi.listReports(reportStatus).then(r=>r.data), enabled: tab==='reports' })
  const { data: modUsersData } = useQuery({ queryKey:['mod-users'], queryFn: ()=>moderationApi.listModeratedUsers().then(r=>r.data), enabled: tab==='moderation' })
  const { data: instBansData } = useQuery({ queryKey:['instance-bans'], queryFn: ()=>moderationApi.listInstanceBans().then(r=>r.data), enabled: tab==='moderation' })
  const { data: fedData }  = useQuery({ queryKey:['admin-fed'],      queryFn: ()=>adminApi.listInstances().then(r=>r.data), enabled: tab==='federation' })
  const { data: invData }  = useQuery({ queryKey:['admin-invites'],  queryFn: ()=>adminApi.listInvites().then(r=>r.data), enabled: tab==='invites' })
  const { data: rulesData } = useQuery({ queryKey:['admin-rules'],   queryFn: ()=>adminApi.listRules().then(r=>r.data),  enabled: tab==='rules' })
  const { data: waitlistData } = useQuery({ queryKey:['admin-waitlist'], queryFn: ()=>adminApi.listWaitlist().then(r=>r.data), enabled: tab==='waitlist' })
  const { data: adminPagesData, refetch: refetchAdminPages } = useQuery({ queryKey:['admin-pages'], queryFn: ()=>pagesApi.list({}).then(r=>r.data), enabled: tab==='pages' })
  const adminPages: any[] = adminPagesData?.pages ?? []
  const verifyPage  = useMutation({ mutationFn: ({slug,v}:{slug:string,v:boolean})=>adminPagesApi.verify(slug,v),  onSuccess:()=>refetchAdminPages() })
  const featurePage = useMutation({ mutationFn: ({slug,v}:{slug:string,v:boolean})=>adminPagesApi.feature(slug,v), onSuccess:()=>refetchAdminPages() })

  useEffect(() => { if (settings) setSettingsForm(settings) }, [settings])

  const saveSettings = useMutation({ mutationFn: ()=>adminApi.updateSettings(settingsForm), onSuccess:()=>{ ok('Saved'); qc.invalidateQueries({queryKey:['instance-info']}) } })
  const setRole      = useMutation({ mutationFn: ({id,role}:{id:string,role:string})=>adminApi.setRole(id,role), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const delUser      = useMutation({ mutationFn: (id:string)=>adminApi.deleteUser(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const reviewRep    = useMutation({ mutationFn: ({id,action,notes}:{id:string,action:string,notes?:string})=>moderationApi.reviewReport(id,{action,notes}), onSuccess:(_,{action})=>{ ok(`Report ${action}`); qc.invalidateQueries({queryKey:['admin-reports']}) } })
  const suspendUser  = useMutation({ mutationFn: ({id,data}:{id:string,data:any})=>moderationApi.suspendUser(id,data), onSuccess:()=>{ ok('User suspended'); qc.invalidateQueries({queryKey:['admin-reports']}); qc.invalidateQueries({queryKey:['mod-users']}) } })
  const unsuspend    = useMutation({ mutationFn: (id:string)=>moderationApi.unsuspendUser(id), onSuccess:()=>{ ok('Unsuspended'); qc.invalidateQueries({queryKey:['mod-users']}) } })
  const banUser      = useMutation({ mutationFn: ({id,data}:{id:string,data:any})=>moderationApi.banUser(id,data), onSuccess:()=>{ ok('User banned'); qc.invalidateQueries({queryKey:['admin-reports']}); qc.invalidateQueries({queryKey:['mod-users']}) } })
  const unban        = useMutation({ mutationFn: (id:string)=>moderationApi.unbanUser(id), onSuccess:()=>{ ok('Unbanned'); qc.invalidateQueries({queryKey:['mod-users']}) } })
  const banInst      = useMutation({ mutationFn: (data:any)=>moderationApi.banInstance(data), onSuccess:()=>{ ok('Instance banned'); setInstanceBanForm({instance:'',reason:'',notes:''}); qc.invalidateQueries({queryKey:['instance-bans']}) } })
  const unbanInst    = useMutation({ mutationFn: (id:string)=>moderationApi.unbanInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['instance-bans']}) })
  const blockInst    = useMutation({ mutationFn: (id:string)=>adminApi.blockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const unblockInst  = useMutation({ mutationFn: (id:string)=>adminApi.unblockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const createInvite = useMutation({ mutationFn: ()=>adminApi.createInvite(), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })
  const revokeInvite   = useMutation({ mutationFn: (id:string)=>adminApi.revokeInvite(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })
  const resendVerif    = useMutation({ mutationFn: (id:string)=>adminApi.resendVerification(id) })
  const approveWait    = useMutation({ mutationFn: (id:string)=>adminApi.approveWaitlist(id), onSuccess:()=>{ ok('Approved — invite sent'); qc.invalidateQueries({queryKey:['admin-waitlist']}) } })
  const rejectWait     = useMutation({ mutationFn: (id:string)=>adminApi.rejectWaitlist(id),  onSuccess:()=>{ ok('Rejected'); qc.invalidateQueries({queryKey:['admin-waitlist']}) } })

  const tabs = [
    { id:'overview',    label:'Overview',    icon: BookOpen },
    { id:'settings',    label:'Settings',    icon: Settings },
    { id:'users',       label:'Users',       icon: Users },
    { id:'waitlist',    label:'Waitlist',    icon: Clock },
    { id:'reports',     label:'Reports',     icon: Flag },
    { id:'moderation',  label:'Moderation',  icon: ShieldAlert },
    { id:'federation',  label:'Federation',  icon: Link2 },
    { id:'invites',     label:'Invites',     icon: Ticket },
    { id:'rules',       label:'Rules',       icon: List },
    { id:'pages',       label:'Pages',       icon: Star },
  ]

  const sf = (k:string) => (e:React.ChangeEvent<HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement>) =>
    setSettingsForm(f=>({...f,[k]:e.target.value}))

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Admin Panel</h1>
      {msg && <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 rounded-lg px-3 py-2 text-sm text-green-700 dark:text-green-400">{msg}</div>}

      <div className="flex gap-1 flex-wrap">
        {tabs.map(({id,label,icon:Icon}) => (
          <button key={id} onClick={()=>setTab(id as any)}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${tab===id?'bg-agora-700 text-white':'btn-secondary'}`}>
            <Icon size={14}/>{label}
          </button>
        ))}
      </div>

      {tab==='overview' && stats && (
        <div className="grid grid-cols-2 gap-3">
          {([['Total Users',stats.total_users],['Posts Today',stats.posts_today],['Active (7d)',stats.active_users_7d],['Pending Reports',stats.pending_reports]] as [string,number][]).map(([k,v])=>(
            <div key={k} className="card p-4">
              <p className="text-2xl font-bold">{v}</p>
              <p className="text-sm text-agora-500">{k}</p>
            </div>
          ))}
        </div>
      )}

      {tab==='settings' && (
        <div className="card p-4 space-y-4">
          {([{k:'instance_name',label:'Instance name'},{k:'instance_description',label:'Description'}] as {k:string,label:string}[]).map(({k,label})=>(
            <div key={k}><label className="label">{label}</label>
              <input className="input" autoComplete="off" value={settingsForm[k]||''} onChange={sf(k)} /></div>
          ))}

          {/* Logo upload */}
          <div>
            <label className="label">Instance logo</label>
            <div className="flex items-center gap-4 mt-1">
              <div className="w-14 h-14 rounded-xl bg-agora-100 dark:bg-agora-700 overflow-hidden flex items-center justify-center flex-shrink-0 border border-agora-200 dark:border-agora-600">
                {settingsForm.logo_url
                  ? <img src={settingsForm.logo_url} alt="Logo" className="w-full h-full object-cover" />
                  : <span className="text-2xl font-bold text-agora-400">{(settingsForm.instance_name||'A')[0].toUpperCase()}</span>
                }
              </div>
              <div className="space-y-1.5">
                <label className="btn-secondary text-sm cursor-pointer">
                  Upload logo
                  <input type="file" accept="image/*" className="hidden" onChange={async e => {
                    const file = e.target.files?.[0]; if (!file) return
                    try {
                      const res = await instanceApi.uploadLogo(file)
                      setSettingsForm(f => ({ ...f, logo_url: res.data.url }))
                    } catch { ok('Upload failed') }
                    e.target.value = ''
                  }} />
                </label>
                {settingsForm.logo_url && (
                  <button className="text-xs text-red-500 hover:underline block"
                    onClick={() => setSettingsForm(f => ({ ...f, logo_url: '' }))}>
                    Remove logo
                  </button>
                )}
                <p className="text-xs text-agora-400">Shown in the sidebar. Square images work best.</p>
              </div>
            </div>
          </div>
          <div><label className="label">Registration mode</label>
            <select className="input" value={settingsForm.registration_mode||'open'} onChange={sf('registration_mode')}>
              <option value="open">Open</option>
              <option value="waitlist">Waitlist</option>
              <option value="invite">Invite only</option>
              <option value="closed">Closed</option>
            </select></div>
          <div><label className="label">Deletion grace period (days)</label>
            <input type="number" className="input" autoComplete="off" value={settingsForm.deletion_grace_days||'30'} onChange={sf('deletion_grace_days')} /></div>
          <div className="flex items-center justify-between py-2">
            <div><p className="font-medium text-sm">Enable federation</p>
              <p className="text-xs text-agora-400">Allow connecting with other Agora instances</p></div>
            <button onClick={()=>setSettingsForm(f=>({...f,federation_enabled:f.federation_enabled==='true'?'false':'true'}))}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors ${settingsForm.federation_enabled==='true'?'bg-agora-700':'bg-agora-200'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${settingsForm.federation_enabled==='true'?'translate-x-5':'translate-x-0'}`} />
            </button>
          </div>
          <div className="flex items-center justify-between py-2">
            <div><p className="font-medium text-sm">Allow users to invite friends</p>
              <p className="text-xs text-agora-400">Users can send email invitations to friends</p></div>
            <button onClick={()=>setSettingsForm(f=>({...f,user_invites_enabled:f.user_invites_enabled==='true'?'false':'true'}))}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors ${settingsForm.user_invites_enabled==='true'?'bg-agora-700':'bg-agora-200'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${settingsForm.user_invites_enabled==='true'?'translate-x-5':'translate-x-0'}`} />
            </button>
          </div>
          <hr className="border-agora-100 dark:border-agora-700"/>
          <h3 className="font-semibold text-sm">SMTP (email)</h3>
          {([{k:'smtp_host',label:'Host',type:'text'},{k:'smtp_port',label:'Port',type:'number'},{k:'smtp_user',label:'Username',type:'text'},{k:'smtp_password',label:'Password',type:'password'},{k:'smtp_from',label:'From address',type:'email'}] as {k:string,label:string,type:string}[]).map(({k,label,type})=>(
            <div key={k}><label className="label">{label}</label>
              <input type={type} className="input" autoComplete={type === 'password' ? 'current-password' : type === 'email' ? 'email' : 'off'} value={settingsForm[k]||''} onChange={sf(k)} /></div>
          ))}
          <div className="flex items-center justify-between py-1">
            <p className="text-sm font-medium">Enable email</p>
            <button onClick={()=>setSettingsForm(f=>({...f,smtp_enabled:f.smtp_enabled==='true'?'false':'true'}))}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors ${settingsForm.smtp_enabled==='true'?'bg-agora-700':'bg-agora-200'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${settingsForm.smtp_enabled==='true'?'translate-x-5':'translate-x-0'}`} />
            </button>
          </div>
          <button onClick={()=>saveSettings.mutate()} disabled={saveSettings.isPending} className="btn-primary">{saveSettings.isPending?'Saving…':'Save settings'}</button>
        </div>
      )}

      {tab==='users' && (
        <div className="space-y-2">
          {(usersData?.users||[]).map((u:any)=>(
            <div key={u.id} className="card p-3 flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <p className="font-medium text-sm">{u.display_name||u.username} <span className="text-agora-400 font-normal">@{u.username}</span></p>
                <p className="text-xs text-agora-400">{u.email} · {u.role}{u.is_suspended?' · 🚫 suspended':''}</p>
              </div>
              <select value={u.role} onChange={e=>setRole.mutate({id:u.id,role:e.target.value})}
                className="text-xs border border-agora-200 rounded px-1.5 py-1 dark:border-agora-600 dark:bg-agora-800">
                <option value="user">User</option><option value="moderator">Mod</option><option value="admin">Admin</option>
              </select>
              {!u.email_verified && (
                <button onClick={()=>resendVerif.mutate(u.id)} className="text-xs text-blue-500 hover:underline" title="Resend verification email">Resend email</button>
              )}
              <button onClick={()=>{if(confirm(`Delete ${u.username}?`))delUser.mutate(u.id)}} className="text-xs text-red-500 hover:underline">Delete</button>
            </div>
          ))}
        </div>
      )}

      {tab==='reports' && (
        <div className="space-y-3">
          <div className="flex gap-2">
            {['pending','actioned','dismissed'].map(s => (
              <button key={s} onClick={() => setReportStatus(s)}
                className={`text-xs px-3 py-1 rounded-full border transition-colors ${reportStatus===s ? 'bg-agora-700 text-white border-agora-700' : 'border-agora-200 dark:border-agora-600 text-agora-500 hover:border-agora-400'}`}>
                {s.charAt(0).toUpperCase()+s.slice(1)}
              </button>
            ))}
          </div>
          {(repsData?.reports||[]).length===0 && <div className="card p-8 text-center text-agora-400">No {reportStatus} reports.</div>}
          {(repsData?.reports||[]).map((r:any) => {
            const sf = suspendForm[r.reported_user_username] || {days:'1',reason:'',notes:''}
            const bf = banForm[r.reported_user_username] || {reason:'',notes:''}
            return (
              <div key={r.id} className="card p-4 space-y-3">
                {/* Report header */}
                <div className="space-y-1">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-xs font-bold uppercase tracking-wide text-red-500 bg-red-50 dark:bg-red-900/20 px-2 py-0.5 rounded-full">
                      {r.violation_type?.replace(/_/g,' ') || r.reason}
                    </span>
                    {r.rule_text && <span className="text-xs text-agora-400 italic">Rule: {r.rule_text}</span>}
                    {r.is_suspended && <span className="text-xs text-amber-600 bg-amber-50 dark:bg-amber-900/20 px-2 py-0.5 rounded-full">⚠️ Already suspended</span>}
                    {r.is_banned && <span className="text-xs text-red-600 bg-red-50 dark:bg-red-900/20 px-2 py-0.5 rounded-full">🚫 Already banned</span>}
                  </div>
                  <p className="text-xs text-agora-400">
                    by @{r.reporter_username}
                    {r.reported_user_username && ` against @${r.reported_user_username}`}
                    {r.reported_post_id && ' (post)'}
                    {r.reported_comment_id && ' (comment)'}
                    {' · '}{new Date(r.created_at).toLocaleDateString()}
                  </p>
                  {r.post_content && (
                    <div className="p-2 bg-agora-50 dark:bg-agora-700/50 rounded border-l-2 border-agora-300 dark:border-agora-500">
                      <p className="text-xs text-agora-400 font-medium mb-0.5">{r.reported_comment_id ? 'Comment' : 'Post'} content</p>
                      <p className="text-sm text-agora-700 dark:text-agora-200 line-clamp-3">{r.post_content}</p>
                    </div>
                  )}
                  {r.details && <p className="text-sm text-agora-600 dark:text-agora-300"><span className="text-xs font-medium text-agora-400">Reporter note: </span>{r.details}</p>}
                </div>

                {/* Actions — only shown on pending reports */}
                {reportStatus === 'pending' && (
                  <div className="border-t border-agora-100 dark:border-agora-700 pt-3 space-y-3">

                    {/* No reported user — just dismiss or mark actioned with notes */}
                    {!r.reported_user_username && (
                      <div className="flex items-center gap-2">
                        <input className="input text-xs py-1 px-2 flex-1" autoComplete="off" placeholder="Notes (optional)"
                          value={reportNotes[r.id] || ''}
                          onChange={e => setReportNotes(n => ({...n, [r.id]: e.target.value}))} />
                        <button onClick={() => reviewRep.mutate({id:r.id, action:'actioned', notes:reportNotes[r.id]})}
                          className="btn-primary text-xs py-1 px-3">Mark actioned</button>
                        <button onClick={() => reviewRep.mutate({id:r.id, action:'dismissed', notes:reportNotes[r.id]})}
                          className="btn-secondary text-xs py-1 px-3">Dismiss</button>
                      </div>
                    )}

                    {/* Reported user — suspend or ban are the primary actions */}
                    {r.reported_user_username && !r.is_banned && (
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        {/* Suspend */}
                        {!r.is_suspended && (
                          <div className="space-y-1.5 p-3 rounded-lg bg-amber-50 dark:bg-amber-900/10 border border-amber-200 dark:border-amber-800">
                            <p className="text-xs font-bold text-amber-700 dark:text-amber-400 uppercase tracking-wide">⏸ Suspend @{r.reported_user_username}</p>
                            <div className="flex gap-1.5">
                              <input className="input text-xs py-1 px-2 flex-1" autoComplete="off" placeholder="Reason shown to user"
                                value={sf.reason}
                                onChange={e => setSuspendForm(f=>({...f,[r.reported_user_username]:{...sf,reason:e.target.value}}))} />
                              <input className="input text-xs py-1 px-2 w-16" autoComplete="off" placeholder="Days" type="number" min="0"
                                value={sf.days}
                                onChange={e => setSuspendForm(f=>({...f,[r.reported_user_username]:{...sf,days:e.target.value}}))} />
                            </div>
                            <input className="input text-xs py-1 px-2 w-full" autoComplete="off" placeholder="Admin notes (private, not shown to user)"
                              value={sf.notes}
                              onChange={e => setSuspendForm(f=>({...f,[r.reported_user_username]:{...sf,notes:e.target.value}}))} />
                            <button disabled={!sf.reason || suspendUser.isPending}
                              onClick={() => suspendUser.mutate({id: r.reported_user_id, data: {
                                reason: sf.reason, notes: sf.notes, days: parseInt(sf.days)||0
                              }})}
                              className="w-full text-xs py-1.5 px-3 rounded-lg bg-amber-500 hover:bg-amber-600 text-white font-semibold disabled:opacity-40 transition-colors">
                              Suspend {sf.days && sf.days!=='0' ? `for ${sf.days} day${sf.days==='1'?'':'s'}` : 'indefinitely'}
                            </button>
                          </div>
                        )}

                        {/* Ban */}
                        <div className="space-y-1.5 p-3 rounded-lg bg-red-50 dark:bg-red-900/10 border border-red-200 dark:border-red-800">
                          <p className="text-xs font-bold text-red-700 dark:text-red-400 uppercase tracking-wide">🚫 Ban @{r.reported_user_username}</p>
                          <input className="input text-xs py-1 px-2 w-full" autoComplete="off" placeholder="Reason shown to user"
                            value={bf.reason}
                            onChange={e => setBanForm(f=>({...f,[r.reported_user_username]:{...bf,reason:e.target.value}}))} />
                          <input className="input text-xs py-1 px-2 w-full" autoComplete="off" placeholder="Admin notes (private)"
                            value={bf.notes}
                            onChange={e => setBanForm(f=>({...f,[r.reported_user_username]:{...bf,notes:e.target.value}}))} />
                          {r.is_remote && r.remote_instance && (
                            <label className="flex items-center gap-2 text-xs text-red-600 cursor-pointer">
                              <input type="checkbox" className="rounded" />
                              Also ban instance ({r.remote_instance})
                            </label>
                          )}
                          <button disabled={!bf.reason || banUser.isPending}
                            onClick={() => { if (confirm(`Permanently ban @${r.reported_user_username}?`)) banUser.mutate({id: r.reported_user_id, data: {reason: bf.reason, notes: bf.notes}}) }}
                            className="w-full text-xs py-1.5 px-3 rounded-lg bg-red-500 hover:bg-red-600 text-white font-semibold disabled:opacity-40 transition-colors">
                            Permanently ban
                          </button>
                        </div>
                      </div>
                    )}

                    {/* Dismiss without action */}
                    <div className="flex items-center gap-2 pt-1 border-t border-agora-100 dark:border-agora-700">
                      <p className="text-xs text-agora-400 flex-1">No action needed?</p>
                      <input className="input text-xs py-1 px-2 w-48" autoComplete="off" placeholder="Dismissal notes (optional)"
                        value={reportNotes[r.id] || ''}
                        onChange={e => setReportNotes(n => ({...n, [r.id]: e.target.value}))} />
                      <button onClick={() => reviewRep.mutate({id:r.id, action:'dismissed', notes:reportNotes[r.id]})}
                        className="btn-secondary text-xs py-1 px-3 flex-shrink-0">Dismiss report</button>
                    </div>
                  </div>
                )}

                {/* Reviewed report — show what was done */}
                {reportStatus !== 'pending' && r.review_notes && (
                  <div className="border-t border-agora-100 dark:border-agora-700 pt-2">
                    <p className="text-xs text-agora-400">
                      {r.reviewer_username && `Reviewed by @${r.reviewer_username}`}
                      {r.reviewed_at && ` · ${new Date(r.reviewed_at).toLocaleDateString()}`}
                    </p>
                    {r.review_notes && <p className="text-xs text-agora-500 mt-0.5">{r.review_notes}</p>}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {tab==='moderation' && (
        <div className="space-y-6">
          {/* Suspended / Banned users */}
          <div>
            <h3 className="font-semibold mb-3">Suspended & Banned Users</h3>
            {(modUsersData?.users||[]).length===0 && (
              <div className="card p-6 text-center text-agora-400 text-sm">No suspended or banned users.</div>
            )}
            {(modUsersData?.users||[]).map((u:any) => (
              <div key={u.id} className="card p-3 mb-2 flex items-start justify-between gap-3 flex-wrap">
                <div className="space-y-0.5">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-sm">@{u.username}</span>
                    {u.is_suspended && !u.banned_at && (
                      <span className="text-xs text-amber-600 bg-amber-50 dark:bg-amber-900/20 px-2 py-0.5 rounded-full">
                        Suspended{u.suspension_expires_at ? ` until ${new Date(u.suspension_expires_at).toLocaleDateString()}` : ' (indefinite)'}
                      </span>
                    )}
                    {u.banned_at && (
                      <span className="text-xs text-red-600 bg-red-50 dark:bg-red-900/20 px-2 py-0.5 rounded-full">Banned</span>
                    )}
                    {u.is_remote && <span className="text-xs text-agora-400">{u.remote_instance}</span>}
                  </div>
                  {u.suspension_reason && <p className="text-xs text-agora-500">Reason: {u.suspension_reason}</p>}
                  {u.ban_reason && <p className="text-xs text-agora-500">Ban reason: {u.ban_reason}</p>}
                  {(u.suspension_notes || u.ban_notes) && (
                    <p className="text-xs text-agora-400 italic">Notes: {u.suspension_notes || u.ban_notes}</p>
                  )}
                </div>
                <div className="flex gap-2 flex-shrink-0">
                  {u.is_suspended && !u.banned_at && (
                    <button onClick={()=>unsuspend.mutate(u.id)} className="btn-secondary text-xs py-1 px-2">Unsuspend</button>
                  )}
                  {u.banned_at && (
                    <button onClick={()=>unban.mutate(u.id)} className="btn-secondary text-xs py-1 px-2">Unban</button>
                  )}
                  {!u.banned_at && (
                    <button onClick={()=>banUser.mutate({id:u.id,data:{reason:'Admin action',notes:''}})}
                      className="btn-danger text-xs py-1 px-2">Ban</button>
                  )}
                </div>
              </div>
            ))}
          </div>

          {/* Instance bans */}
          <div>
            <h3 className="font-semibold mb-3">Instance Bans</h3>
            <div className="card p-4 space-y-3 mb-3">
              <p className="text-sm font-medium">Ban an instance</p>
              <div className="flex gap-2 flex-wrap">
                <input className="input text-sm flex-1 min-w-40" autoComplete="off" placeholder="Instance domain (e.g. bad.example.com)"
                  value={instanceBanForm.instance} onChange={e=>setInstanceBanForm(f=>({...f,instance:e.target.value}))} />
                <input className="input text-sm flex-1 min-w-40" autoComplete="off" placeholder="Reason"
                  value={instanceBanForm.reason} onChange={e=>setInstanceBanForm(f=>({...f,reason:e.target.value}))} />
                <input className="input text-sm flex-1 min-w-40" autoComplete="off" placeholder="Admin notes (private)"
                  value={instanceBanForm.notes} onChange={e=>setInstanceBanForm(f=>({...f,notes:e.target.value}))} />
                <button onClick={()=>banInst.mutate(instanceBanForm)} disabled={!instanceBanForm.instance||!instanceBanForm.reason||banInst.isPending}
                  className="btn-danger text-sm">Ban instance</button>
              </div>
            </div>
            {(instBansData?.bans||[]).length===0 && (
              <div className="card p-4 text-center text-agora-400 text-sm">No instance bans.</div>
            )}
            {(instBansData?.bans||[]).map((b:any) => (
              <div key={b.id} className="card p-3 mb-2 flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium">{b.instance}</p>
                  <p className="text-xs text-agora-400">{b.reason}{b.banned_by&&` · by @${b.banned_by}`}</p>
                  {b.notes && <p className="text-xs text-agora-400 italic">{b.notes}</p>}
                </div>
                <button onClick={()=>unbanInst.mutate(b.id)} className="btn-secondary text-xs py-1 px-2 flex-shrink-0">Remove</button>
              </div>
            ))}
          </div>
        </div>
      )}

      {tab==='federation' && (
        <FederationPanel
          instances={fedData?.instances||[]}
          onAdd={()=>qc.invalidateQueries({queryKey:['admin-fed']})}
          onBlock={(id)=>blockInst.mutate(id)}
          onUnblock={(id)=>unblockInst.mutate(id)}
        />
      )}

      {tab==='invites' && (
        <div className="space-y-3">
          <button onClick={()=>createInvite.mutate()} disabled={createInvite.isPending} className="btn-primary">Generate invite code</button>
          {(invData?.invites||[]).map((inv:any)=>(
            <div key={inv.id} className="card p-3 flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <code className="text-sm font-mono bg-agora-100 dark:bg-agora-700 px-2 py-0.5 rounded">{inv.code}</code>
                <p className="text-xs text-agora-400 mt-1">by @{inv.created_by_username}{inv.used_by_username?` · used by @${inv.used_by_username}`:' · unused'}</p>
              </div>
              <button onClick={()=>navigator.clipboard.writeText(inv.code)} className="btn-ghost text-xs">Copy</button>
              {!inv.used_at&&<button onClick={()=>revokeInvite.mutate(inv.id)} className="text-xs text-red-500 hover:underline">Revoke</button>}
            </div>
          ))}
        </div>
      )}

      {tab==='rules' && (
        <RulesPanel rules={rulesData?.rules ?? []} onChanged={()=>qc.invalidateQueries({queryKey:['admin-rules']})} />
      )}

      {tab==='waitlist' && (
        <div className="space-y-4">
          <div className="card p-4">
            <h3 className="font-semibold mb-1">Waitlist Queue</h3>
            <p className="text-sm text-agora-500 mb-4">Users who signed up while the instance is in waitlist mode. Oldest first. Approving sends them an invite link by email.</p>
            {(waitlistData?.users ?? []).length === 0 ? (
              <p className="text-sm text-agora-400 italic py-4 text-center">No one on the waitlist right now.</p>
            ) : (
              <div className="divide-y divide-agora-100 dark:divide-agora-800">
                {(waitlistData?.users ?? []).map((u: any) => (
                  <div key={u.id} className="flex items-center gap-3 py-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm">{u.display_name}</span>
                        <span className="text-agora-400 text-xs">@{u.username}</span>
                        {!u.email_verified && (
                          <span className="text-xs bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 px-1.5 py-0.5 rounded">unverified email</span>
                        )}
                      </div>
                      <div className="text-xs text-agora-400">{u.email} · Joined {new Date(u.created_at).toLocaleDateString()}</div>
                    </div>
                    <div className="flex gap-2 flex-shrink-0">
                      <button
                        onClick={() => approveWait.mutate(u.id)}
                        disabled={approveWait.isPending}
                        className="text-xs bg-agora-600 hover:bg-agora-700 text-white px-3 py-1.5 rounded-lg font-medium transition-colors"
                      >
                        Approve
                      </button>
                      <button
                        onClick={() => { if (confirm(`Reject ${u.username}? This will permanently delete their account.`)) rejectWait.mutate(u.id) }}
                        disabled={rejectWait.isPending}
                        className="text-xs bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/40 text-red-600 dark:text-red-400 px-3 py-1.5 rounded-lg font-medium transition-colors"
                      >
                        Reject
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Pages tab (AGORA-114) */}
      {tab==='pages' && (
        <div className="space-y-4">
          <div className="card p-4">
            <h3 className="font-semibold mb-1">Page Moderation</h3>
            <p className="text-sm text-agora-500 mb-4">Verify notable pages and feature them in the discovery section. Verified and featured status survive owner edits.</p>
            {adminPages.length === 0 ? (
              <p className="text-sm text-agora-400 italic py-4 text-center">No pages yet.</p>
            ) : (
              <div className="divide-y divide-agora-100 dark:divide-agora-800">
                {adminPages.map((p: any) => (
                  <div key={p.id} className="flex items-center gap-3 py-3">
                    <div className="w-9 h-9 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                      {p.avatar_url
                        ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
                        : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-500">{p.display_name[0]}</span>}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm">{p.display_name}</span>
                        <span className="text-agora-400 text-xs">@{p.slug}</span>
                        {p.is_verified && <span className="text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-600 px-1.5 py-0.5 rounded">✓ Verified</span>}
                        {p.is_featured && <span className="text-xs bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 px-1.5 py-0.5 rounded flex items-center gap-0.5"><Star size={10} /> Featured</span>}
                      </div>
                      <div className="text-xs text-agora-400">{p.subscriber_count} subscribers · {p.post_count} posts</div>
                    </div>
                    <div className="flex gap-2 flex-shrink-0">
                      <button
                        onClick={() => verifyPage.mutate({ slug: p.slug, v: !p.is_verified })}
                        disabled={verifyPage.isPending}
                        className={`text-xs px-2.5 py-1.5 rounded-lg font-medium transition-colors ${
                          p.is_verified
                            ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 hover:bg-blue-200'
                            : 'bg-agora-100 dark:bg-agora-700 text-agora-600 hover:bg-agora-200'
                        }`}>
                        {p.is_verified ? '✓ Verified' : 'Verify'}
                      </button>
                      <button
                        onClick={() => featurePage.mutate({ slug: p.slug, v: !p.is_featured })}
                        disabled={featurePage.isPending}
                        className={`text-xs px-2.5 py-1.5 rounded-lg font-medium transition-colors ${
                          p.is_featured
                            ? 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 hover:bg-yellow-200'
                            : 'bg-agora-100 dark:bg-agora-700 text-agora-600 hover:bg-agora-200'
                        }`}>
                        {p.is_featured ? '★ Featured' : '☆ Feature'}
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ── Rules Panel ───────────────────────────────────────────────────────────────

function RulesPanel({ rules, onChanged }: { rules: any[], onChanged: () => void }) {
  const [newText, setNewText] = useState('')
  const [editingId, setEditingId] = useState<string|null>(null)
  const [editText, setEditText] = useState('')

  const create = useMutation({
    mutationFn: () => adminApi.createRule(newText),
    onSuccess: () => { setNewText(''); onChanged() },
  })
  const update = useMutation({
    mutationFn: () => adminApi.updateRule(editingId!, editText),
    onSuccess: () => { setEditingId(null); onChanged() },
  })
  const remove = useMutation({
    mutationFn: (id: string) => adminApi.deleteRule(id),
    onSuccess: onChanged,
  })
  const move = useMutation({
    mutationFn: ({ id, direction }: { id: string, direction: 'up'|'down' }) => adminApi.moveRule(id, direction),
    onSuccess: onChanged,
  })

  return (
    <div className="space-y-4">
      <div className="card p-4 space-y-3">
        <h3 className="font-semibold">Instance Rules</h3>
        <p className="text-sm text-agora-500">Rules are shown to users when they register and when filing reports.</p>

        {rules.length === 0 && (
          <p className="text-sm text-agora-400 italic">No rules yet. Add your first rule below.</p>
        )}

        <div className="space-y-2">
          {rules.map((rule: any, i: number) => (
            <div key={rule.id} className="flex items-start gap-2 bg-agora-50 dark:bg-agora-700/50 rounded-lg px-3 py-2.5">
              <span className="text-sm font-bold text-agora-400 w-5 flex-shrink-0 mt-0.5">{i + 1}.</span>
              <div className="flex-1 min-w-0">
                {editingId === rule.id ? (
                  <div className="space-y-2">
                    <textarea
                      className="input w-full text-sm resize-none"
                      rows={2}
                      autoComplete="off"
                      value={editText}
                      onChange={e => setEditText(e.target.value)}
                      autoFocus
                    />
                    <div className="flex gap-2">
                      <button onClick={() => update.mutate()} disabled={!editText.trim() || update.isPending}
                        className="btn-primary text-xs py-1 px-3">Save</button>
                      <button onClick={() => setEditingId(null)} className="btn-secondary text-xs py-1 px-3">Cancel</button>
                    </div>
                  </div>
                ) : (
                  <p className="text-sm">{rule.text}</p>
                )}
              </div>
              {editingId !== rule.id && (
                <div className="flex items-center gap-1 flex-shrink-0">
                  <button onClick={() => move.mutate({ id: rule.id, direction: 'up' })} disabled={i === 0}
                    className="btn-ghost p-1 text-agora-400 disabled:opacity-30" title="Move up">↑</button>
                  <button onClick={() => move.mutate({ id: rule.id, direction: 'down' })} disabled={i === rules.length - 1}
                    className="btn-ghost p-1 text-agora-400 disabled:opacity-30" title="Move down">↓</button>
                  <button onClick={() => { setEditingId(rule.id); setEditText(rule.text) }}
                    className="btn-ghost p-1 text-agora-400 hover:text-agora-600 text-xs">Edit</button>
                  <button onClick={() => { if (confirm('Delete this rule?')) remove.mutate(rule.id) }}
                    className="btn-ghost p-1 text-red-400 hover:text-red-600 text-xs">Delete</button>
                </div>
              )}
            </div>
          ))}
        </div>

        {/* Add new rule */}
        <div className="pt-2 border-t border-agora-100 dark:border-agora-700 space-y-2">
          <label className="label">Add a rule</label>
          <textarea
            className="input w-full text-sm resize-none"
            rows={2}
            autoComplete="off"
            placeholder="e.g. No harassment or hate speech"
            value={newText}
            onChange={e => setNewText(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && e.metaKey && newText.trim()) create.mutate() }}
          />
          <button
            onClick={() => create.mutate()}
            disabled={!newText.trim() || create.isPending}
            className="btn-primary text-sm"
          >
            {create.isPending ? 'Adding…' : 'Add Rule'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Federation Panel ──────────────────────────────────────────────────────────

function FederationPanel({ instances, onAdd, onBlock, onUnblock }: {
  instances: any[], onAdd: () => void,
  onBlock: (id: string) => void, onUnblock: (id: string) => void
}) {
  const [domain, setDomain] = useState('')
  const [addMsg, setAddMsg] = useState('')
  const [addErr, setAddErr] = useState('')

  const add = useMutation({
    mutationFn: () => adminApi.addInstance(domain.trim()),
    onSuccess: (res) => {
      setAddMsg(`✓ Added ${res.data.name || res.data.domain}`)
      setDomain('')
      setAddErr('')
      onAdd()
      setTimeout(() => setAddMsg(''), 4000)
    },
    onError: (e: any) => setAddErr(e.response?.data?.error || 'Could not add instance'),
  })

  return (
    <div className="space-y-4">
      {/* Add instance */}
      <div className="card p-4 space-y-3">
        <h3 className="font-semibold">Add Federated Instance</h3>
        <p className="text-sm text-agora-500">
          Enter the domain of another Agora instance to federate with. Both instances must have federation enabled.
        </p>
        {addMsg && <p className="text-sm text-green-600">{addMsg}</p>}
        {addErr && <p className="text-sm text-red-500">{addErr}</p>}
        <div className="flex gap-2">
          <input
            className="input flex-1 text-sm"
            autoComplete="off"
            placeholder="social.example.com"
            value={domain}
            onChange={e => { setDomain(e.target.value); setAddErr('') }}
            onKeyDown={e => e.key === 'Enter' && domain.trim() && add.mutate()}
          />
          <button
            onClick={() => add.mutate()}
            disabled={!domain.trim() || add.isPending}
            className="btn-primary text-sm"
          >
            {add.isPending ? 'Connecting…' : 'Add Instance'}
          </button>
        </div>
      </div>

      {/* Instance list */}
      <div className="space-y-2">
        {instances.length === 0 && (
          <div className="card p-8 text-center text-agora-400 space-y-1">
            <p className="font-medium">No federated instances yet</p>
            <p className="text-sm">Add an instance above to start federating.</p>
          </div>
        )}
        {instances.map((inst: any) => (
          <div key={inst.id} className={`card p-3 flex items-center gap-3 ${inst.status === 'blocked' ? 'opacity-60' : ''}`}>
            <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 flex items-center justify-center flex-shrink-0 text-sm font-bold text-agora-500">
              {(inst.name || inst.domain)[0].toUpperCase()}
            </div>
            <div className="flex-1 min-w-0">
              <p className="font-medium text-sm truncate">{inst.name || inst.domain}</p>
              <p className="text-xs text-agora-400">
                {inst.domain}
                <span className={`ml-2 px-1.5 py-0.5 rounded text-xs ${inst.status === 'active' ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400' : 'bg-red-100 dark:bg-red-900/30 text-red-600'}`}>
                  {inst.status}
                </span>
              </p>
            </div>
            {inst.status === 'active'
              ? <button onClick={() => onBlock(inst.id)} className="text-xs text-red-500 hover:underline flex-shrink-0">Block</button>
              : <button onClick={() => onUnblock(inst.id)} className="text-xs text-green-600 hover:underline flex-shrink-0">Unblock</button>}
          </div>
        ))}
      </div>
    </div>
  )
}
