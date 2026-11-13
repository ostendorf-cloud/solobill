// The one place this app builds govue's API client, and the one place the
// current session's CSRF token lives: GET /api/v1/me hands it back on
// every successful call (see internal/httpapi/auth_handlers.go's
// meResponse), and csrfToken is what createClient reads it from on every
// unsafe request afterwards, per govue's own client contract.
import { ref } from 'vue'
import { createClient } from '@ostendorf-cloud/govue'

export const csrfToken = ref<string | undefined>(undefined)

export const apiClient = createClient({
  csrfToken: () => csrfToken.value,
})
