import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi, moderationApi } from '../api'
import { Users, Settings, Flag, Link2, Ticket, BookOpen } from 'lucide-react'

export default function AdminPage() {
  const [tab, setTab] = useState<'overview'|'settings'|'users'|'reports'|'federation'|'invites'>('overview')
  const [settingsForm, setSettingsForm] = useState<Record<string,string>>({})
  const [msg, setMsg] = useState('')
  const qc = useQueryClient()
  const ok = (m:string) => { setMsg(m); setTimeout(()=>setMsg(''), 3000) }

  const { data: stats }    = useQuery({ queryKey:['admin-stats'],    queryFn: ()=>adminApi.getStats().then(r=>r.data),    enabled: tab==='overview' })
  const { data: settings } = useQuery({ queryKey:['admin-settings'], queryFn: ()=>adminApi.getSettings().then(r=>r.data), enabled: tab==='settings' })
  const { data: usersData }= useQuery({ queryKey:['admin-users'],    queryFn: ()=>adminApi.listUsers().then(r=>r.data),   enabled: tab==='users' })
  const { data: repsData } = useQuery({ queryKey:['admin-reports'],  queryFn: ()=>moderationApi.listReports('pending').then(r=>r.data), enabled: tab==='reports' })
  const { data: fedData }  = useQuery({ queryKey:['admin-fed'],      queryFn: ()=>adminApi.listInstances().then(r=>r.data), enabled: tab==='federation' })
  const { data: invData }  = useQuery({ queryKey:['admin-invites'],  queryFn: ()=>adminApi.listInvites().then(r=>r.data), enabled: tab==='invites' })

  // Populate settings form when data loads (RQ v5: no onSuccess in useQuery)
  useEffect(() => { if (settings) setSettingsForm(settings) }, [settings])

  const saveSettings = useMutation({ mutationFn: ()=>adminApi.updateSettings(settingsForm), onSuccess:()=>ok('Saved') })
  const setRole      = useMutation({ mutationFn: ({id,role}:{id:string,role:string})=>adminApi.setRole(id,role), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const delUser      = useMutation({ mutationFn: (id:string)=>adminApi.deleteUser(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const reviewRep    = useMutation({ mutationFn: ({id,action}:{id:string,action:string})=>moderationApi.reviewReport(id,{action}), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-reports']}) })
  const blockInst    = useMutation({ mutationFn: (id:string)=>adminApi.blockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const unblockInst  = useMutation({ mutationFn: (id:string)=>adminApi.unblockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const createInvite = useMutation({ mutationFn: ()=>adminApi.createInvite(), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })
  const revokeInvite = useMutation({ mutationFn: (id:string)=>adminApi.revokeInvite(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })

  const tabs = [
    { id:'overview',   label:'Overview',    icon: BookOpen },
    { id:'settings',   label:'Settings',    icon: Settings },
    { id:'users',      label:'Users',       icon: Users },
    { id:'reports',    label:'Reports',     icon: Flag },
    { id:'federation', label:'Federation',  icon: Link2 },
    { id:'invites',    label:'Invites',     icon: Ticket },
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
              <input className="input" value={settingsForm[k]||''} onChange={sf(k)} /></div>
          ))}
          <div><label className="label">Registration mode</label>
            <select className="input" value={settingsForm.registration_mode||'open'} onChange={sf('registration_mode')}>
              <option value="open">Open</option>
              <option value="invite">Invite only</option>
              <option value="closed">Closed</option>
            </select></div>
          <div><label className="label">Deletion grace period (days)</label>
            <input type="number" className="input" value={settingsForm.deletion_grace_days||'30'} onChange={sf('deletion_grace_days')} /></div>
          <div className="flex items-center justify-between py-2">
            <div><p className="font-medium text-sm">Enable federation</p>
              <p className="text-xs text-agora-400">Allow connecting with other Agora instances</p></div>
            <button onClick={()=>setSettingsForm(f=>({...f,federation_enabled:f.federation_enabled==='true'?'false':'true'}))}
              className={`relative inline-flex h-6 w-11 rounded-full transition-colors ${settingsForm.federation_enabled==='true'?'bg-agora-700':'bg-agora-200'}`}>
              <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${settingsForm.federation_enabled==='true'?'translate-x-5':'translate-x-0'}`} />
            </button>
          </div>
          <hr className="border-agora-100 dark:border-agora-700"/>
          <h3 className="font-semibold text-sm">SMTP (email)</h3>
          {([{k:'smtp_host',label:'Host',type:'text'},{k:'smtp_port',label:'Port',type:'number'},{k:'smtp_user',label:'Username',type:'text'},{k:'smtp_password',label:'Password',type:'password'},{k:'smtp_from',label:'From address',type:'email'}] as {k:string,label:string,type:string}[]).map(({k,label,type})=>(
            <div key={k}><label className="label">{label}</label>
              <input type={type} className="input" value={settingsForm[k]||''} onChange={sf(k)} /></div>
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
              <button onClick={()=>{if(confirm(`Delete ${u.username}?`))delUser.mutate(u.id)}} className="text-xs text-red-500 hover:underline">Delete</button>
            </div>
          ))}
        </div>
      )}

      {tab==='reports' && (
        <div className="space-y-2">
          {(repsData?.reports||[]).length===0 && <div className="card p-8 text-center text-agora-400">No pending reports.</div>}
          {(repsData?.reports||[]).map((r:any)=>(
            <div key={r.id} className="card p-3 space-y-2">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">{r.reason}</p>
                  <p className="text-xs text-agora-400">by @{r.reporter_username}{r.reported_user_username&&` against @${r.reported_user_username}`}</p>
                  {r.details&&<p className="text-xs text-agora-500 mt-1">{r.details}</p>}
                </div>
                <div className="flex gap-2 flex-shrink-0">
                  <button onClick={()=>reviewRep.mutate({id:r.id,action:'actioned'})} className="btn-primary text-xs py-1 px-2">Action</button>
                  <button onClick={()=>reviewRep.mutate({id:r.id,action:'dismissed'})} className="btn-secondary text-xs py-1 px-2">Dismiss</button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {tab==='federation' && (
        <div className="space-y-2">
          {(fedData?.instances||[]).length===0&&<div className="card p-8 text-center text-agora-400">No federated instances yet.</div>}
          {(fedData?.instances||[]).map((inst:any)=>(
            <div key={inst.id} className="card p-3 flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <p className="font-medium text-sm">{inst.name||inst.domain}</p>
                <p className="text-xs text-agora-400">{inst.domain} · {inst.status}</p>
              </div>
              {inst.status==='active'
                ?<button onClick={()=>blockInst.mutate(inst.id)} className="text-xs text-red-500 hover:underline">Block</button>
                :<button onClick={()=>unblockInst.mutate(inst.id)} className="text-xs text-green-600 hover:underline">Unblock</button>}
            </div>
          ))}
        </div>
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
    </div>
  )
}
