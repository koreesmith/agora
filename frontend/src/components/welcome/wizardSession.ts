// AGORA-388: Layout sends an account that hasn't finished the getting started
// wizard to /welcome, but only once per browser session. Otherwise any link
// out of the wizard (Settings, a profile, Help) would bounce straight back to
// it. A later visit brings the wizard back until it's finished or skipped.

const SESSION_KEY = 'agora_welcome_shown'

export function markWelcomeShown() {
  try { sessionStorage.setItem(SESSION_KEY, '1') } catch { /* storage unavailable */ }
}

export function welcomeShownThisSession(): boolean {
  try { return sessionStorage.getItem(SESSION_KEY) === '1' } catch { return false }
}
