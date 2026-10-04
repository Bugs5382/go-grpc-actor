# AGENTS.md - go-grpc-actor

Guide for AI agents working in this repository. Pair with `CLAUDE.md` (the working agreement and
hook-enforced rules). Keep this file current when the build, layout, or public API changes.

## What this is

Carry the signed-in user and the act-as admin across gRPC service hops, trusted only from authenticated internal callers.

A Go library: gRPC client and server interceptors that carry an `Actor` (the effective user plus,
during act-as, the real admin) across service hops. The one thing to understand before changing it:
a server reads a forwarded actor only when its `TrustFunc` passes, and the default trusts nobody.
Never add a path that reads actor metadata without that check.

## Using go-grpc-actor

- The edge calls `WithActor` after authenticating the user; handlers read `FromContext` or
  `Require`.
- Every internal dial installs `UnaryClientInterceptor` and `StreamClientInterceptor`; every server
  installs `UnaryServerInterceptor` and `StreamServerInterceptor` with `WithTrust`.
- `TrustSPIFFEIDs` and `TrustDNSNames` need a server that verifies client certificates.
- The wire format (`MetadataKey`, the JSON object with `"v":1`) is a contract between services
  running different versions. Change it only with a new version number that old servers refuse.

## Layout

- `actor.go` - the `Actor` type, `Validate`, `WithActor`, `FromContext`, `Require`
- `wire.go` - `MetadataKey` and the JSON encoding
- `client.go`, `server.go` - the interceptors and server options
- `trust.go` - `TrustFunc`, `TrustNone`, the mTLS helpers, `AnyOf`, `ForMethods`
- `errors.go` - the sentinels, `Codes`, and the gRPC status mapping
- `*_test.go` - black-box tests over bufconn with a throwaway CA (`helpers_test.go`)

## Build, test, lint

- Build: `task build`
- Test: `task test`; also run `go test -race ./...` (the concurrency test is meant for it). No
  external services: every test runs over bufconn.
- Lint: `task lint` (gofmt, golangci-lint, yamllint)
- License headers: `task license` (verify), `task license:fix` (inject)

## Logging

Follow the logging rules in `CLAUDE.md`. In short:

- Log generously: entry and exit of significant operations, decisions and branches, retries, state
  changes, external calls (target, duration, outcome), and every error with its context.
- Levels: `trace` for step-by-step detail, `debug` for flow, `info` for lifecycle, `warn` and
  `error` for problems. The environment filters the volume, so err on the side of too much.
- Environments: local dev `trace` with `LOG_FORMAT=console` (never JSON), dev cluster `debug`,
  qa/staging `info`, production `error`. Every cluster environment logs JSON. Set levels through
  `LOG_LEVEL` and `LOG_FORMAT`, never in code; local settings live in the run target or
  `.env.example`.
- Never log secrets, tokens, or personal data, not even at `trace`. Log an opaque or keyed ID.

## Conventions and gotchas

- See `CLAUDE.md` for the branch/commit/PR rules; they are enforced by the git hooks in
  `.claude/hooks` (run `bash .claude/hooks/install.sh` once per clone).
- Open every PR as a draft. CI skips drafts, so run the full checks locally, push once they pass,
  and mark the PR ready when the work is finished; see CLAUDE.md "CI and Actions minutes".
- The library does not log. Decisions go to the consumer through `WithObserver`, so the root
  module stays free of a logging dependency.
- Test identities are fakes under `example.org`. Never use real workload IDs or hosts in tests.
