/// <reference types="vite/client" />
// Loads the `persist` option augmentation for defineStore(). Without it the
// option is only visible in programs that reach main.ts, so the vitest project
// (tests plus their imports) fails to type-check the persisted stores.
/// <reference types="pinia-plugin-persistedstate" />
