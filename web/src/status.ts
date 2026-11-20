/** The shape of GET /api/v1/status (docs/api.md, "Status"), served by
 *  issue #75. The app shell of issue #72 fetches it and shows the GoBD
 *  banner; its tests stub the fetch with a StatusResponse fixture or a
 *  failed response, so #72 needs no running #75 to be tested. */
export interface ObjectStoreStatus {
  reachable: boolean
  versioning: boolean
  object_lock: boolean
  require_versioning: boolean
  file_routes_available: boolean
  gobd_protection_active: boolean
  checked_at: string
}

export interface StatusResponse {
  object_store: ObjectStoreStatus
}

/** gobdBannerVisible decides the banner from a fetched status. null means
 *  the status is unknown: the endpoint answered an error (501 before #75
 *  merged, 401 before login) or the request failed. An unknown status
 *  shows no banner, because nothing claims the protection is missing. */
export function gobdBannerVisible(status: StatusResponse | null): boolean {
  return status !== null && !status.object_store.gobd_protection_active
}
