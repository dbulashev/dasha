// appBasePath reads the runtime prefix from the HTML base element.
export function appBasePath(): string {
  return document.querySelector('base')?.getAttribute('href') || '/'
}

export function appURL(path: string): string {
  return appBasePath().replace(/\/$/, '') + path
}
