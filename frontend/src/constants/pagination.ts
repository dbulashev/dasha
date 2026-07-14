/** Default page size for paginated tables; the user can override it in settings. */
export const DEFAULT_PAGE_SIZE = 15

/** Page sizes offered in the settings dialog. */
export const PAGE_SIZE_OPTIONS = [10, 15, 25, 50, 100]

/**
 * Tables with compact rows (e.g. connection sources) fit proportionally more
 * rows, so they scale the chosen page size instead of pinning a size of their
 * own — the old LARGE_PAGE_SIZE (30) was exactly twice the default.
 */
export const LARGE_PAGE_FACTOR = 2
