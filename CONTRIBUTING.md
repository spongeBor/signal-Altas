# Contributing

- Go 1.26 and Node.js 24; run `go -C api test ./...` and `npx vue-tsc --noEmit` before opening a PR.
- Keep collection read-only: never add a code path that writes to a monitored device.
- New adapters implement the collector interface and must return a clear error when credentials or
  a host key are missing.
- UI text is Chinese today; keep new strings in the same language unless you are adding a translated
  key (see `web/src/i18n.ts`).
