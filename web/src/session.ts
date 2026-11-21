// The shell's view of the signed in session: who is signed in
// (GET /api/v1/me) and the object store status (GET /api/v1/status), which
// decides the GoBD banner. The state is shared so the shell, which loads it,
// and a view that meets a 401, which clears it, use the same path. The CSRF
// token lives next to it: it belongs to the session and goes with it.
import { computed, ref } from 'vue'

import { apiClient, csrfToken } from './api'
import type { StatusResponse } from './status'

export interface MeResponse {
  email: string
  name: string
  role: string
  csrf_token: string
}

export const me = ref<MeResponse | null>(null)
export const status = ref<StatusResponse | null>(null)

/** sessionReady is true once /me answered: the CSRF token is then stored,
 *  so a mutating request can carry it. */
export const sessionReady = computed(() => me.value !== null)

/** sessionFailed is true after the last /me answered an error, so a view
 *  can say why a mutating action stays disabled. */
export const sessionFailed = ref(false)

// A load that was started before a clear must not write its answer back.
let generation = 0

/** clearSession forgets the user, the status and the CSRF token. */
export function clearSession(): void {
  generation++
  sessionFailed.value = false
  me.value = null
  status.value = null
  csrfToken.value = undefined
}

/** loadSession reads /me and then /status. A failed /me means not signed
 *  in. A failed /status (501 before the endpoint exists, 401) means the
 *  status is unknown and shows no banner. */
export async function loadSession(): Promise<void> {
  const current = ++generation
  sessionFailed.value = false
  let response: MeResponse
  try {
    response = await apiClient.get<MeResponse>('/me')
  } catch {
    // Not signed in, or the session store is down. The views send the
    // visitor to the login on a 401 themselves and explain a disabled save.
    if (current === generation) {
      sessionFailed.value = true
    }
    return
  }
  if (current !== generation) {
    return
  }
  csrfToken.value = response.csrf_token
  me.value = response
  let answer: StatusResponse | null
  try {
    answer = await apiClient.get<StatusResponse>('/status')
  } catch {
    answer = null
  }
  if (current === generation) {
    status.value = answer
  }
}
