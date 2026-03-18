// Returns true if the URL points to a GIF image
export function isGifUrl(url: string): boolean {
  if (!url) return false
  try {
    const u = new URL(url)
    const host = u.hostname.toLowerCase()
    const path = u.pathname.toLowerCase()

    // Direct .gif file extension
    if (path.endsWith('.gif')) return true

    // Giphy CDNs
    if (host.includes('giphy.com')) return true

    // Tenor CDNs — tenor.com and tenor.co, including media subdomains
    if (host.endsWith('tenor.com') || host.endsWith('tenor.co')) return true

    // Gfycat
    if (host.includes('gfycat.com')) return true

    // Imgur GIFs (gifv or direct gif)
    if (host.includes('imgur.com') && (path.endsWith('.gif') || path.endsWith('.gifv'))) return true

  } catch {
    // Fallback for malformed URLs
    const lower = url.toLowerCase()
    if (lower.includes('.gif') || lower.includes('tenor.com') || lower.includes('tenor.co') || lower.includes('giphy.com')) return true
  }
  return false
}

// Returns true if the URL is a direct GIF media file (not a share page)
// Share pages like tenor.com/xPpM.gif or giphy.com/gifs/name serve HTML, not images
export function isDirectGifUrl(url: string): boolean {
  if (!url) return false
  try {
    const u = new URL(url)
    const host = u.hostname.toLowerCase()
    const path = u.pathname.toLowerCase()

    // Direct media CDN subdomains — these serve actual GIF files
    if (host.startsWith('media.tenor.com') || host.startsWith('media1.tenor.') || host.startsWith('c.tenor.com')) return true
    if (host.startsWith('media.giphy.com') || host.startsWith('media0.giphy.com') || host.startsWith('media1.giphy.com') || host.startsWith('media2.giphy.com') || host.startsWith('media3.giphy.com') || host.startsWith('media4.giphy.com')) return true
    if (host.includes('gfycat.com') && path.endsWith('.gif')) return true
    if (host.includes('imgur.com') && path.endsWith('.gif')) return true

    // Direct .gif URLs on non-share-page hosts
    if (path.endsWith('.gif') && !host.match(/^(www\.)?(tenor|giphy)\.com$/)) return true

  } catch {}
  return false
}
