/**
 * "Only the newest answer counts". Responses of async calls may arrive out of order; call begin()
 * before each request and apply the response only if isLatest() still holds. Keys are independent:
 * a newer request for one project never invalidates a request for another.
 */
export interface LatestTracker {
  begin(key: string): number
  isLatest(key: string, token: number): boolean
}

export function createLatestTracker(): LatestTracker {
  const counters = new Map<string, number>()
  return {
    begin(key) {
      const next = (counters.get(key) ?? 0) + 1
      counters.set(key, next)
      return next
    },
    isLatest(key, token) {
      return counters.get(key) === token
    },
  }
}
