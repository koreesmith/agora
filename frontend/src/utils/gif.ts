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
