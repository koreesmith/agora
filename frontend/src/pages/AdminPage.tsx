import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi, moderationApi } from '../api'
import { Users, Settings, Flag, Link2, Ticket, BookOpen, List } from 'lucide-react'

export default function AdminPage() {
  const [tab, setTab] = useState<'overview'|'settings'|'users'|'reports'|'federation'|'invites'|'rules'>('overview')
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
  const { data: rulesData } = useQuery({ queryKey:['admin-rules'],   queryFn: ()=>adminApi.listRules().then(r=>r.data),  enabled: tab==='rules' })

  // Populate settings form when data loads (RQ v5: no onSuccess in useQuery)
  useEffect(() => { if (settings) setSettingsForm(settings) }, [settings])

  const saveSettings = useMutation({ mutationFn: ()=>adminApi.updateSettings(settingsForm), onSuccess:()=>ok('Saved') })
  const setRole      = useMutation({ mutationFn: ({id,role}:{id:string,role:string})=>adminApi.setRole(id,role), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const delUser      = useMutation({ mutationFn: (id:string)=>adminApi.deleteUser(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-users']}) })
  const reviewRep    = useMutation({ mutationFn: ({id,action}:{id:string,action:string})=>moderationApi.reviewReport(id,{action}), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-reports']}) })
  const blockInst    = useMutation({ mutationFn: (id:string)=>adminApi.blockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const unblockInst  = useMutation({ mutationFn: (id:string)=>adminApi.unblockInstance(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-fed']}) })
  const createInvite = useMutation({ mutationFn: ()=>adminApi.createInvite(), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })
  const revokeInvite   = useMutation({ mutationFn: (id:string)=>adminApi.revokeInvite(id), onSuccess:()=>qc.invalidateQueries({queryKey:['admin-invites']}) })
  const resendVerif    = useMutation({ mutationFn: (id:string)=>adminApi.resendVerification(id) })

  const tabs = [
    { id:'overview',   label:'Overview',    icon: BookOpen },
    { id:'settings',   label:'Settings',    icon: Settings },
    { id:'users',      label:'Users',       icon: Users },
    { id:'reports',    label:'Reports',     icon: Flag },
    { id:'federation', label:'Federation',  icon: Link2 },
    { id:'invites',    label:'Invites',     icon: Ticket },
    { id:'rules',      label:'Rules',       icon: List },
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
          <div><label className="label">Registration mode</label>
            <select className="input" value={settingsForm.registration_mode||'open'} onChange={sf('registration_mode')}>
              <option value="open">Open</option>
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
