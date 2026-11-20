import { describe, expect, it } from 'vitest'

import { gobdBannerVisible, type StatusResponse } from './status'

function status(active: boolean): StatusResponse {
  return {
    object_store: {
      reachable: true,
      versioning: active,
      object_lock: active,
      require_versioning: true,
      file_routes_available: active,
      gobd_protection_active: active,
      checked_at: '2026-12-01T10:00:00Z',
    },
  }
}

describe('gobdBannerVisible', () => {
  it('shows the banner while the protection is not active', () => {
    expect(gobdBannerVisible(status(false))).toBe(true)
  })

  it('hides the banner while the protection is active', () => {
    expect(gobdBannerVisible(status(true))).toBe(false)
  })

  it('hides the banner while the status is unknown', () => {
    expect(gobdBannerVisible(null)).toBe(false)
  })
})
