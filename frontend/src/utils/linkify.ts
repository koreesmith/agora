// URL detection shared by renderContent and the composer's link preview
// (AGORA-375). Besides full http(s):// URLs it recognizes a bare domain such
// as "bit.ly/4jFFzI0" or "example.com" and links it to https://, the way
// Bluesky and most social clients do. The backend (internal/linkify) and the
// mobile app (utils/linkify.ts) apply the same rules, so keep the three in
// step.

// Regex source for one URL token, safe to drop into a larger alternation:
// it has no capturing groups, since String.split splices every capture into
// its output. A bare-domain match is only a candidate until linkHref has also
// checked its TLD and the character before it (no lookbehind, which older
// Safari versions reject at parse time).
export const URL_TOKEN =
  'https?:\\/\\/[^\\s<>"{}|\\\\^`[\\]]+' +
  '|[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*\\.[a-zA-Z]{2,63}(?::[0-9]{2,5})?(?:\\/[^\\s<>"{}|\\\\^`[\\]]*)?'

// TLDs a bare domain may end in. A full IANA list would also link filenames
// such as "readme.md", "setup.py" or "install.sh" (all real ccTLDs), so
// those are deliberately left out; such a link still works with https://.
const COMMON_TLDS = new Set(`
  com net org edu gov mil int info biz name pro
  io co ly me tv fm am gg gl to be ai app dev xyz
  online site tech blog news social page link club store shop live
  space world website art media network cloud email today life
  wiki zone lol fyi community foundation games
  us uk ca au nz ie de fr es it nl se no fi dk ch at cz pt gr hu ro
  jp cn kr tw hk sg in id my th vn ph br ar mx cl za ng ke eu ru ua
  tr il ae sa`.trim().split(/\s+/))

const SCHEME_RE = /^https?:\/\//i
const BARE_HOST_RE = /^[a-z0-9.-]*\.([a-z]{2,63})(?::[0-9]+)?(?:\/|$)/i
// A bare domain right after one of these is the tail of some other token:
// an email address, @handle, #tag, +group, or word.
const JOINED_RE = /[A-Za-z0-9@#+_./:-]$/

// Trailing punctuation likely belongs to the sentence, not the URL.
export function trimUrl(token: string): string {
  return token.replace(/[.,!?)]+$/, '')
}

// linkHref returns where an already-trimmed URL token links to, or null if it
// is not a link. before is the text immediately preceding the token.
export function linkHref(url: string, before = ''): string | null {
  if (SCHEME_RE.test(url)) return url
  if (JOINED_RE.test(before)) return null
  const m = BARE_HOST_RE.exec(url)
  if (!m || !COMMON_TLDS.has(m[1].toLowerCase())) return null
  return `https://${url}`
}

// findFirstUrl returns the first link in text: href is where it goes, and
// text is how it appears in the input (they differ for a scheme-less URL).
export function findFirstUrl(text: string): { href: string; text: string } | null {
  const re = new RegExp(URL_TOKEN, 'g')
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    const url = trimUrl(m[0])
    const href = linkHref(url, text.slice(0, m.index))
    if (href) return { href, text: url }
  }
  return null
}
