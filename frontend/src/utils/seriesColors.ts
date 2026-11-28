// Categorical slots as [light, dark]; the dark step is picked for the dark
// surface, not derived. Both orders are validated for colour-vision deficiency.
export const SERIES_SLOTS: readonly (readonly [string, string])[] = [
  ['#2a78d6', '#3987e5'],
  ['#eb6834', '#d95926'],
  ['#1baf7a', '#199e70'],
  ['#eda100', '#c98500'],
  ['#e87ba4', '#d55181'],
  ['#008300', '#008300'],
  ['#4a3aa7', '#9085e9'],
  ['#e34948', '#e66767'],
]

export const NEUTRAL_SLOT: readonly [string, string] = ['#6d6c66', '#a8a79c']

export function paletteColor(index: number, dark: boolean): string {
  return (SERIES_SLOTS[index] ?? NEUTRAL_SLOT)[dark ? 1 : 0]
}

export function neutralColor(dark: boolean): string {
  return NEUTRAL_SLOT[dark ? 1 : 0]
}
