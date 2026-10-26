import { useEffect, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '../../api'

// AGORA-380: admin controls for the remote media cache, the local copies of
// fediverse avatars and covers. Lives in the Media tab next to the orphan
// scanner, which deliberately leaves the cache's own files alone.

interface CacheSettings { enabled: boolean, max_mb: number, max_file_mb: number }
interface CacheStats { files: number, bytes: number, orphaned_files: number }

const mb = (bytes: number) => (bytes / 1024 / 1024).toFixed(1)

export default function RemoteMediaCacheCard({ ok }: { ok: (msg: string) => void }) {
  const qc = useQueryClient()
  const { data } = useQuery<{ settings: CacheSettings, stats: CacheStats }>({
    queryKey: ['admin-remote-media-cache'],
    queryFn: () => adminApi.getRemoteMediaCache().then(r => r.data),
  })
  const [form, setForm] = useState<CacheSettings | null>(null)
  useEffect(() => { if (data && !form) setForm(data.settings) }, [data, form])

  const refresh = (next: any) => qc.setQueryData(['admin-remote-media-cache'], (old: any) => ({ ...old, ...next }))
  const save = useMutation({
    mutationFn: (s: CacheSettings) => adminApi.updateRemoteMediaCache(s),
    onSuccess: (res) => { refresh(res.data); setForm(res.data.settings); ok('Cache settings saved') },
    onError: (e: any) => ok(e.response?.data?.error || 'Could not save cache settings'),
  })
  const prune = useMutation({
    mutationFn: () => adminApi.pruneRemoteMediaCache(),
    onSuccess: (res) => {
      refresh({ stats: res.data.stats })
      const r = res.data.result
      ok(`Pruned ${r.orphans_removed + r.evicted} file(s), freed ${mb(r.bytes_freed)} MB`)
    },
    onError: () => ok('Prune failed'),
  })
  const clear = useMutation({
    mutationFn: () => adminApi.clearRemoteMediaCache(),
    onSuccess: (res) => { refresh({ stats: res.data.stats }); ok(`Cleared the cache, freed ${mb(res.data.result.bytes_freed)} MB`) },
    onError: () => ok('Clear failed'),
  })

  const stats = data?.stats
  const limitBytes = (data?.settings.max_mb ?? 0) * 1024 * 1024
  const pct = stats && limitBytes ? Math.min(100, (stats.bytes / limitBytes) * 100) : 0
  const dirty = !!form && !!data && JSON.stringify(form) !== JSON.stringify(data.settings)
  const busy = prune.isPending || clear.isPending

  return (
    <div className="card p-4 space-y-4">
      <div>
        <h3 className="font-semibold">Remote Media Cache</h3>
        <p className="text-sm text-agora-500 mt-0.5">
          Local copies of profile pictures and cover photos from other servers, so they keep working when the original
          link expires or is removed. Unused copies are pruned hourly, and the oldest are evicted when the cache passes its limit.
        </p>
      </div>

      {stats && (
        <div className="space-y-1.5">
          <div className="flex items-baseline justify-between text-sm">
            <span>
              <span className="font-semibold">{stats.files}</span> file{stats.files !== 1 ? 's' : ''} · {mb(stats.bytes)} MB of {data?.settings.max_mb} MB
            </span>
            {stats.orphaned_files > 0 && (
              <span className="text-xs text-agora-400">{stats.orphaned_files} unused, pruned next run</span>
            )}
          </div>
          <div className="h-2 rounded-full bg-agora-100 dark:bg-agora-800 overflow-hidden">
            <div className={`h-full ${pct >= 90 ? 'bg-amber-500' : 'bg-agora-500'}`} style={{ width: `${pct}%` }} />
          </div>
        </div>
      )}

      {form && (
        <div className="space-y-3">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="rounded" checked={form.enabled}
              onChange={e => setForm({ ...form, enabled: e.target.checked })} />
            Cache remote profile pictures and covers
          </label>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="label" htmlFor="remote-cache-max-mb">Total size limit (MB)</label>
              <input id="remote-cache-max-mb" type="number" min="1" className="input" autoComplete="off" value={form.max_mb}
                onChange={e => setForm({ ...form, max_mb: Number(e.target.value) })} />
            </div>
            <div>
              <label className="label" htmlFor="remote-cache-max-file-mb">Largest single file (MB)</label>
              <input id="remote-cache-max-file-mb" type="number" min="1" className="input" autoComplete="off" value={form.max_file_mb}
                onChange={e => setForm({ ...form, max_file_mb: Number(e.target.value) })} />
            </div>
          </div>
          {!form.enabled && (
            <p className="text-xs text-agora-400">
              While off, nothing new is cached and existing copies stay until pruned or cleared. Profiles without a copy load from the original server.
            </p>
          )}
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        <button className="btn-primary text-sm" disabled={!dirty || save.isPending} onClick={() => form && save.mutate(form)}>
          {save.isPending ? 'Saving…' : 'Save'}
        </button>
        <button className="btn-secondary text-sm" disabled={busy} onClick={() => prune.mutate()}>
          {prune.isPending ? 'Pruning…' : 'Prune now'}
        </button>
        <button className="btn-danger text-sm" disabled={busy || !stats?.files}
          onClick={() => { if (confirm('Remove every cached copy? Profiles fall back to the original links until they are cached again.')) clear.mutate() }}>
          {clear.isPending ? 'Clearing…' : 'Clear cache'}
        </button>
      </div>
    </div>
  )
}
