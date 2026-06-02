// Authentication module — token issue/verify with a small in-memory store.
import { createHash, randomBytes } from "node:crypto"

export interface Session {
  readonly id: string
  readonly userId: number
  readonly expiresAt: number // epoch ms
}

const TTL_MS = 1000 * 60 * 30 // 30 minutes
const store = new Map<string, Session>()

/** Issue a fresh session token for a user. */
export function issue(userId: number): Session {
  const id = randomBytes(16).toString("hex")
  const session: Session = { id, userId, expiresAt: Date.now() + TTL_MS }
  store.set(id, session)
  return session
}

/** Verify a token, returning the userId or `null` when invalid/expired. */
export function verify(token: string): number | null {
  const session = store.get(token)
  if (!session) return null
  if (session.expiresAt < Date.now()) {
    store.delete(token)
    return null
  }
  return session.userId
}

export function fingerprint(token: string): string {
  return createHash("sha256").update(token).digest("hex").slice(0, 12)
}
