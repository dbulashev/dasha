// Значение задаёт HTML <base>, который контейнер формирует при запуске.
export function appBasePath(): string {
  return document.querySelector('base')?.getAttribute('href') || '/'
}

export function appURL(path: string): string {
  return appBasePath().replace(/\/$/, '') + path
}
