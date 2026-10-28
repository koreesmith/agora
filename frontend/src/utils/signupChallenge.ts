import { authApi } from '../api'

// AGORA-407: the signup proof-of-work puzzle. The server hands out
// sha256(salt + secret number) and we find the number by trying each one,
// in the background while the person fills in the form.

export interface SignupSolution {
  algorithm: string
  challenge: string
  number: number
  salt: string
  signature: string
}

async function sha256Hex(text: string): Promise<string> {
  const buf = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))
  return Array.from(new Uint8Array(buf), b => b.toString(16).padStart(2, '0')).join('')
}

// Resolves to the solution, or null when the instance has the check off or
// anything goes wrong. A null just means the signup goes to the waitlist,
// so it's never worth blocking the form on.
export async function solveSignupChallenge(): Promise<SignupSolution | null> {
  try {
    if (!crypto?.subtle) return null
    const res = await authApi.signupChallenge()
    if (!res.data?.enabled) return null
    const { algorithm, challenge, maxnumber, salt, signature } = res.data.challenge
    for (let n = 0; n <= maxnumber; n++) {
      if (await sha256Hex(salt + n) === challenge) {
        return { algorithm, challenge, number: n, salt, signature }
      }
    }
  } catch { /* fall through to the waitlist */ }
  return null
}
