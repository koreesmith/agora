import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Mail, Search, UserPlus, Users2 } from 'lucide-react'
import { atprotoApi, federationApi, friendsApi, groupsApi, onboardingApi, searchApi } from '../../api'
import type { StepProps } from './types'
import { Callout, StepFooter, StepHeader } from './WizardParts'

// AGORA-391: friends on this instance (whoever invited them first, then
// recent members, or a search), follows on Bluesky or the Fediverse by
// handle, and a few popular public groups. Everything here is optional.

interface Person {
  id: string
  username: string
  display_name: string
  avatar_url: string
  // '' | 'pending' (sent) | 'pending_incoming' | 'friends', or the search
  // endpoint's 'accepted' / 'self'.
  friend_status: string
}

function Avatar({ name, url, size = 'w-11 h-11' }: { name: string; url?: string; size?: string }) {
  return (
    <span aria-hidden="true" className={`${size} rounded-full bg-agora-600 text-white flex items-center justify-center font-semibold flex-shrink-0 overflow-hidden`}>
      {url ? <img src={url} alt="" className="w-full h-full object-cover" /> : (name.trim()[0] || '?').toUpperCase()}
    </span>
  )
}

// A Fediverse actor federated through Bridgy Fed is a Bluesky account, as on
// Connections (AGORA-196).
const isBridgedBluesky = (instance?: string) => !!instance && /(^|\.)brid\.gy$/i.test(instance)

export default function FindPeopleStep({ instance, me, setNotes, next, back }: StepProps) {
  const qc = useQueryClient()
  const [status, setStatus] = useState<Record<string, string>>({})
  const [busyId, setBusyId] = useState('')
  const [err, setErr] = useState('')

  // ── People on this instance ──────────────────────────────────────────────
  const { data: suggestions } = useQuery({
    queryKey: ['onboarding-suggestions'],
    queryFn: () => onboardingApi.suggestions().then(r => r.data),
  })
  const [q, setQ] = useState('')
  const [debounced, setDebounced] = useState('')
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 300)
    return () => clearTimeout(t)
  }, [q])
  const { data: results, isFetching: searching } = useQuery({
    queryKey: ['onboarding-search', debounced],
    queryFn: () => searchApi.searchUsers(debounced, 'local').then(r => r.data.users as any[]),
    enabled: debounced.length >= 2,
  })

  const suggested: (Person & { note: string })[] = [
    ...(suggestions?.inviter ? [{ ...suggestions.inviter, note: 'Invited you to Agora' }] : []),
    ...(suggestions?.people ?? []).map((p: Person) => ({ ...p, note: 'Joined recently' })),
  ]
  const found: (Person & { note: string })[] = (results ?? [])
    .filter(u => !u.is_remote && u.friendship_status !== 'self')
    .slice(0, 6)
    .map(u => ({ ...u, friend_status: u.friendship_status, note: `@${u.username}` }))
  const showingSearch = debounced.length >= 2
  const people = showingSearch ? found : suggested

  const befriend = async (p: Person) => {
    const current = status[p.id] ?? p.friend_status
    setBusyId(p.id); setErr('')
    try {
      if (current === 'pending_incoming') {
        await friendsApi.acceptRequest(p.id)
        setStatus(s => ({ ...s, [p.id]: 'friends' }))
      } else {
        await friendsApi.sendRequest(p.id)
        setStatus(s => ({ ...s, [p.id]: 'pending' }))
        setNotes(n => ({ requestsSent: n.requestsSent + 1 }))
      }
      qc.invalidateQueries({ queryKey: ['friends'] })
    } catch (e: any) {
      setErr(e.response?.data?.error || 'Couldn\'t send that friend request.')
    } finally {
      setBusyId('')
    }
  }

  // ── Follow by handle ─────────────────────────────────────────────────────
  // Following on Bluesky comes from the account's own Bluesky identity, so it
  // needs AT Proto on for the account as well as the instance. Fediverse
  // follows only need the instance to speak ActivityPub.
  const canBluesky = !!instance.atproto_enabled && !!me.atproto_enabled
  const canFediverse = !!instance.activitypub_enabled
  const [handle, setHandle] = useState('')
  const [preview, setPreview] = useState<any>(null)
  const [looking, setLooking] = useState(false)
  const [following, setFollowing] = useState(false)
  const [followErr, setFollowErr] = useState('')

  const lookUp = async (raw = handle) => {
    const h = raw.trim().replace(/^@/, '')
    if (!h) return
    const fediverse = h.includes('@') || /^https?:\/\//i.test(h)
    setPreview(null); setFollowing(false); setFollowErr('')
    if (fediverse && !canFediverse) { setFollowErr('This instance doesn\'t connect to the Fediverse.'); return }
    if (!fediverse && !canBluesky) {
      setFollowErr(instance.atproto_enabled
        ? 'Bluesky is off for your account, so you can\'t follow Bluesky accounts. You can turn it back on in the Bluesky step.'
        : 'This instance doesn\'t connect to Bluesky.')
      return
    }
    setLooking(true)
    try {
      const { data } = fediverse
        ? await federationApi.resolveFediverseHandle(h)
        : await atprotoApi.resolveBlueskyHandle(h)
      setPreview({ ...data, network: fediverse ? 'fediverse' : 'bluesky' })
    } catch (e: any) {
      setFollowErr(e.response?.data?.error || 'Couldn\'t find that account. Check the handle and try again.')
    } finally {
      setLooking(false)
    }
  }
  const follow = async () => {
    if (!preview) return
    setLooking(true); setFollowErr('')
    try {
      if (preview.network === 'fediverse') {
        await federationApi.followFediverseAccount(preview.actor_url)
        qc.invalidateQueries({ queryKey: ['fediverse-following'] })
      } else {
        await atprotoApi.followBlueskyAccount(preview.did)
        qc.invalidateQueries({ queryKey: ['bluesky-following'] })
      }
      setFollowing(true)
      setNotes(n => ({ follows: n.follows + 1 }))
    } catch (e: any) {
      setFollowErr(e.response?.data?.error || 'Couldn\'t follow that account.')
    } finally {
      setLooking(false)
    }
  }
  const bridged = preview?.network === 'fediverse' && isBridgedBluesky(preview.instance)

  // ── Groups ───────────────────────────────────────────────────────────────
  const { data: groupsData } = useQuery({
    queryKey: ['onboarding-groups'],
    queryFn: () => groupsApi.list().then(r => r.data),
  })
  const groups: any[] = [...(groupsData?.friend_groups ?? []), ...(groupsData?.popular_groups ?? [])]
    .filter((g: any) => g.privacy === 'public' && !g.is_member)
    .slice(0, 3)
  const [joined, setJoined] = useState<Record<string, boolean>>({})
  const join = async (slug: string) => {
    setBusyId(slug); setErr('')
    try {
      await groupsApi.join(slug)
      setJoined(j => ({ ...j, [slug]: true }))
      setNotes(n => ({ groupsJoined: n.groupsJoined + 1 }))
      qc.invalidateQueries({ queryKey: ['groups'] })
    } catch (e: any) {
      setErr(e.response?.data?.error || 'Couldn\'t join that group.')
    } finally {
      setBusyId('')
    }
  }

  return (
    <>
      <StepHeader title="Find your people">
        Agora is better with friends. Add a few now, or skip this and do it later from Connections.
      </StepHeader>

      {err && (
        <div role="alert" className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
          {err}
        </div>
      )}

      <div className="space-y-3.5">
        <div className="space-y-1">
          <h2 className="font-semibold text-agora-900 dark:text-agora-50">People on {instance.domain || instance.instance_name || 'Agora'}</h2>
          <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">
            Friendships are mutual: once they accept, you'll each see the other's friends-only posts.
          </p>
        </div>
        <div className="relative">
          <label htmlFor="welcome-people-search" className="sr-only">Search people on {instance.domain || 'this instance'}</label>
          <Search size={18} aria-hidden="true" className="absolute left-3.5 top-1/2 -translate-y-1/2 text-agora-500" />
          <input id="welcome-people-search" type="search" className="input min-h-[44px] pl-10" value={q}
            onChange={e => setQ(e.target.value)} placeholder="Search by name or username" autoComplete="off" />
        </div>

        {people.length > 0 ? (
          <ul className="rounded-xl border border-agora-100 dark:border-agora-700 divide-y divide-agora-100 dark:divide-agora-700 overflow-hidden">
            {people.map(p => {
              const st = status[p.id] ?? p.friend_status
              const isFriend = st === 'friends' || st === 'accepted'
              return (
                <li key={p.id} className="flex items-center gap-3.5 px-4 py-3">
                  <Avatar name={p.display_name || p.username} url={p.avatar_url} />
                  <div className="min-w-0 flex-1">
                    <p className="font-semibold text-sm text-agora-900 dark:text-agora-50 truncate">{p.display_name || p.username}</p>
                    <p className="text-sm text-agora-600 dark:text-agora-400 truncate">
                      {p.note.startsWith('@') ? p.note : `@${p.username} · ${p.note}`}
                    </p>
                  </div>
                  {isFriend ? (
                    <span className="inline-flex items-center gap-1.5 min-h-[40px] px-3 text-sm font-semibold text-agora-700 dark:text-agora-200"><Check size={16} aria-hidden="true" /> Friends</span>
                  ) : st === 'pending' ? (
                    <span className="inline-flex items-center gap-1.5 min-h-[40px] px-3 rounded-lg bg-agora-100 dark:bg-agora-700 text-sm font-semibold text-agora-800 dark:text-agora-100"><Check size={16} aria-hidden="true" /> Request sent</span>
                  ) : (
                    <button type="button" onClick={() => befriend(p)} disabled={busyId === p.id} className="btn-primary min-h-[40px] flex-shrink-0">
                      <UserPlus size={16} aria-hidden="true" />
                      {st === 'pending_incoming' ? 'Accept request' : 'Add friend'}
                      <span className="sr-only"> {p.display_name || p.username}</span>
                    </button>
                  )}
                </li>
              )
            })}
          </ul>
        ) : (
          <p className="text-sm text-agora-600 dark:text-agora-400 py-2">
            {showingSearch
              ? (searching ? 'Searching…' : `No one here matches "${debounced}".`)
              : 'No one else has joined yet. Invite someone, or come back later.'}
          </p>
        )}

        {instance.user_invites_enabled === 'true' && (
          <a href="/invite-friend" target="_blank" rel="noopener noreferrer"
            className="inline-flex items-center gap-2 min-h-[44px] text-sm font-semibold text-agora-700 dark:text-agora-200 hover:underline">
            <Mail size={18} aria-hidden="true" /> Invite someone by email
          </a>
        )}
      </div>

      {(canBluesky || canFediverse) && (
        <div className="space-y-3.5 pt-7 border-t border-agora-100 dark:border-agora-700">
          <div className="space-y-1">
            <h2 className="font-semibold text-agora-900 dark:text-agora-50">
              Know people on {canBluesky && canFediverse ? 'Bluesky or the Fediverse' : canBluesky ? 'Bluesky' : 'the Fediverse'}?
            </h2>
            <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">
              Follow anyone by their handle. A follow isn't a friendship: you'll see their public posts, and they won't see your friends-only ones.
            </p>
          </div>
          <form className="flex flex-wrap gap-2.5" onSubmit={e => { e.preventDefault(); lookUp() }}>
            <label htmlFor="welcome-remote-handle" className="sr-only">Handle to follow</label>
            <input id="welcome-remote-handle" className="input min-h-[44px] flex-[1_1_300px] min-w-0" value={handle}
              onChange={e => { setHandle(e.target.value); setPreview(null); setFollowErr('') }} autoComplete="off"
              placeholder={canBluesky && canFediverse ? 'alex.bsky.social or alex@mastodon.social' : canBluesky ? 'alex.bsky.social' : 'alex@mastodon.social'} />
            <button type="submit" disabled={looking || !handle.trim()} className="btn-secondary dark:bg-transparent dark:ring-1 dark:ring-inset dark:ring-agora-500 dark:hover:bg-agora-700 min-h-[44px] px-5">
              {looking && !preview ? 'Looking…' : 'Look up'}
            </button>
          </form>

          {followErr && <p role="alert" className="text-sm text-red-700 dark:text-red-400">{followErr}</p>}

          {preview && bridged && canBluesky && (
            <Callout tone="info">
              @{preview.preferred_username} is a Bluesky account bridged into the Fediverse.{' '}
              <button type="button" onClick={() => { setHandle(preview.preferred_username); lookUp(preview.preferred_username) }}
                className="font-semibold underline">Follow it on Bluesky instead</button>
            </Callout>
          )}

          {preview && !(bridged && canBluesky) && (
            <div className="flex items-center gap-3.5 rounded-xl border border-agora-100 dark:border-agora-700 px-4 py-3">
              <Avatar
                name={preview.network === 'bluesky' ? (preview.display_name || preview.handle || '') : (preview.name || preview.preferred_username || '')}
                url={preview.network === 'bluesky' ? preview.avatar_url : preview.icon_url} />
              <div className="min-w-0 flex-1">
                <p className="font-semibold text-sm text-agora-900 dark:text-agora-50 truncate">
                  {preview.network === 'bluesky' ? (preview.display_name || preview.handle) : (preview.name || preview.preferred_username)}
                </p>
                <p className="text-sm text-agora-600 dark:text-agora-400 truncate">
                  {preview.network === 'bluesky' ? `@${preview.handle} · Bluesky` : `@${preview.preferred_username}@${preview.instance} · Fediverse`}
                </p>
              </div>
              {following
                ? <span className="inline-flex items-center gap-1.5 min-h-[40px] px-3 text-sm font-semibold text-agora-700 dark:text-agora-200"><Check size={16} aria-hidden="true" /> Following</span>
                : <button type="button" onClick={follow} disabled={looking} className="btn-primary min-h-[40px] flex-shrink-0">Follow</button>}
            </div>
          )}

          <p className="text-sm text-agora-600 dark:text-agora-400">
            Their posts show in your main feed. To hide someone from it, turn off the home icon on their row in Connections, or build a feed just for them in My Feeds.
          </p>
        </div>
      )}

      {groups.length > 0 && (
        <div className="space-y-3.5 pt-7 border-t border-agora-100 dark:border-agora-700">
          <div className="space-y-1">
            <h2 className="font-semibold text-agora-900 dark:text-agora-50">Join a group</h2>
            <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">
              Groups are communities around a shared interest. Here are a few popular ones here.
            </p>
          </div>
          <ul className="flex flex-wrap gap-3">
            {groups.map((g: any) => (
              <li key={g.id} className="flex-[1_1_200px] rounded-xl border border-agora-100 dark:border-agora-700 p-4 space-y-3">
                <div className="flex items-center gap-3">
                  <span aria-hidden="true" className="w-10 h-10 rounded-xl bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100 flex items-center justify-center flex-shrink-0 overflow-hidden">
                    {g.avatar_url ? <img src={g.avatar_url} alt="" className="w-full h-full object-cover" /> : <Users2 size={20} />}
                  </span>
                  <div className="min-w-0">
                    <p className="font-semibold text-sm text-agora-900 dark:text-agora-50 truncate">{g.name}</p>
                    <p className="text-xs text-agora-600 dark:text-agora-400">
                      Public group · {g.member_count} {g.member_count === 1 ? 'member' : 'members'}
                    </p>
                  </div>
                </div>
                {joined[g.slug]
                  ? <p className="flex items-center justify-center gap-1.5 min-h-[40px] rounded-lg bg-agora-700 text-white text-sm font-semibold"><Check size={16} aria-hidden="true" /> Joined</p>
                  : <button type="button" onClick={() => join(g.slug)} disabled={busyId === g.slug} className="btn-secondary dark:bg-transparent dark:ring-1 dark:ring-inset dark:ring-agora-500 dark:hover:bg-agora-700 w-full min-h-[40px]">
                      Join<span className="sr-only"> {g.name}</span>
                    </button>}
              </li>
            ))}
          </ul>
        </div>
      )}

      <StepFooter onBack={back} onSkip={next} onContinue={next} />
    </>
  )
}
