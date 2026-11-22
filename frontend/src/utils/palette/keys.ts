// The same test useHotkey makes: there `cmd` means Meta on a Mac and Ctrl elsewhere.
export function isMac(): boolean {
  return typeof navigator !== 'undefined' && navigator.userAgent.includes('Macintosh')
}

export function modKeyLabel(): string {
  return isMac() ? '⌘' : 'Ctrl+'
}

/** Cmd on a Mac, Ctrl elsewhere: what opens a link in a new tab. */
export function hasModKey(e: KeyboardEvent | MouseEvent): boolean {
  return isMac() ? e.metaKey : e.ctrlKey
}

export function hasAnyModifier(e: KeyboardEvent): boolean {
  return e.ctrlKey || e.metaKey || e.altKey || e.shiftKey
}
