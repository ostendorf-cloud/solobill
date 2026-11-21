// The editor asks before it is left with unsaved changes. A move the owner
// confirmed, a lost session (401) or a discarded draft needs no question: the
// editor's guard asks leaveBypassed(), and every such move goes through
// goWithoutPrompt. A counter keeps overlapping moves apart; it is reset when
// the move ends, whether it succeeded or another guard cancelled it.
import type { RouteLocationRaw, Router } from 'vue-router'

let bypass = 0

/** leaveBypassed tells whether a move that must not be questioned is running. */
export function leaveBypassed(): boolean {
  return bypass > 0
}

/** goWithoutPrompt navigates without the editor's unsaved changes question. */
export async function goWithoutPrompt(router: Router, to: RouteLocationRaw): Promise<void> {
  bypass++
  try {
    await router.push(to)
  } finally {
    bypass--
  }
}
