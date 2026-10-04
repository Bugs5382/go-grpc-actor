# go-grpc-actor 🐹

> 🧭 Carry the signed-in user and the act-as admin across gRPC service hops, trusted only from authenticated internal callers.

A request enters at the edge with a signed-in user. During act-as, an admin works as that user,
and audit must still name the admin. A plain gRPC hop drops both. `go-grpc-actor` carries one
`Actor` value through the context and gRPC metadata on every hop, and a receiving service accepts
it only from a caller it has authenticated as an internal service.

## ✨ Highlights

- 🔁 **Survives every hop** — the impersonator travels with the subject, so the third service sees the same admin the edge saw.
- 🔒 **Fail closed** — a server trusts no caller until you say which ones; untrusted actor metadata is stripped or refused.
- 🪪 **mTLS trust built in** — trust a peer by SPIFFE ID or DNS SAN, only with a verified client certificate.
- 🧩 **Pluggable trust** — any `TrustFunc`, such as one backed by a verified workload token, with per-method allow-lists.
- 🔢 **go-apperr codes** — refusals carry your own codes and the right gRPC status.
- 🪶 **Small** — depends on gRPC and `go-apperr` only.

## 📦 Install

```bash
go get github.com/Bugs5382/go-grpc-actor
```

## 🚀 Usage

The edge authenticates the user, then puts the actor in the context. During act-as the real admin
goes in `Impersonator`:

```go
ctx = grpcactor.WithActor(ctx, grpcactor.Actor{
    Subject:      targetUserID, // authorization evaluates as this user
    Impersonator: adminUserID,  // empty when nobody is impersonating
})
```

Every service dials its internal peers with the client interceptors:

```go
conn, err := grpc.NewClient(target,
    grpc.WithTransportCredentials(mtlsCreds),
    grpc.WithChainUnaryInterceptor(grpcactor.UnaryClientInterceptor()),
    grpc.WithChainStreamInterceptor(grpcactor.StreamClientInterceptor()),
)
```

Every internal service serves with the server interceptors and says whom it trusts:

```go
opts := []grpcactor.ServerOption{
    grpcactor.WithTrust(grpcactor.TrustSPIFFEIDs(
        "spiffe://example.org/ns/apps/sa/gateway",
        "spiffe://example.org/ns/apps/sa/workflow",
    )),
    grpcactor.WithRejectUntrusted(),
}
srv := grpc.NewServer(
    grpc.Creds(mtlsServerCreds), // tls.RequireAndVerifyClientCert
    grpc.ChainUnaryInterceptor(grpcactor.UnaryServerInterceptor(opts...)),
    grpc.ChainStreamInterceptor(grpcactor.StreamServerInterceptor(opts...)),
)
```

Handlers read the actor, or require one:

```go
a, err := grpcactor.Require(ctx, codeNoActor) // Unauthenticated, with your apperr code
audit.Record(a.RealUser(), a.Subject)
```

A handler that calls the next service with the context it was given forwards the actor with no
code of its own.

## 🛡️ Trust model

- 🚫 **Nothing is trusted by default.** `TrustNone` is the default, so a server that is not told
  whom to trust ignores every forwarded actor.
- 🧹 **Handlers never see the raw header.** The actor key is removed from the incoming metadata on
  every call, trusted or not; `FromContext` is the only way to read it.
- 🚪 **Edge vs internal.** From an untrusted caller the actor is dropped and the call is served
  without one. With `WithRejectUntrusted` the call is refused with `PermissionDenied` instead, and
  `WithObserver` reports it. Edge services that face clients strip; internal services reject.
- 🪪 **mTLS.** `TrustSPIFFEIDs` trusts a client certificate with exactly one URI SAN equal to a
  listed SPIFFE ID; `TrustDNSNames` trusts a listed DNS SAN. Both refuse a certificate the TLS
  handshake did not verify, so the server must use `tls.RequireAndVerifyClientCert`.
- 🎟️ **Workload tokens.** A service that authenticates callers with a projected service-account
  token writes a `TrustFunc` that reads the verified caller its auth interceptor put in the
  context, and installs this interceptor after that one. `ForMethods` and `AnyOf` turn it into a
  per-method allow-list.
- ✍️ **No signature, on purpose.** Signing the actor with a shared key would add a shared secret
  between services and protect nothing the authenticated channel does not: the trusted caller is
  the one vouching for the actor, and an untrusted caller's actor is never read.
- 🎭 **No roles on the wire.** The actor names people, not grants. Each service works out what the
  subject may do from its own data, so a forwarded actor can never widen a caller's authority.

## 📨 Wire format

One binary metadata key, `x-grpc-actor-bin`, holding one JSON object:

```json
{"v":1,"sub":"user-1001","imp":"admin-7","ten":"tenant-1","sid":"sess-1"}
```

Empty fields are left out. One key keeps the actor atomic, so a subject and an impersonator can
never come from two different senders. A server refuses, with `InvalidArgument`, an actor from a
trusted caller that is not exactly one value, is over 4096 bytes, has an unknown version, has no
subject, impersonates itself, or holds control characters or fields over 256 bytes.

## ❗ Errors

| Sentinel | gRPC status | When |
|---|---|---|
| `ErrNoActor` | `Unauthenticated` | `Require` or `WithRequireActor` found no actor |
| `ErrUntrustedCaller` | `PermissionDenied` | an untrusted caller sent an actor, with `WithRejectUntrusted` |
| `ErrInvalidActor` | `InvalidArgument` | the actor is malformed |

`Require(ctx, code)` and `WithCodes(grpcactor.Codes{...})` attach your own go-apperr codes;
`apperr.Code(err)` reads them back, and `errors.Is` still finds the sentinel.

## 🛠 Develop

```bash
task build    # go build ./...
task test     # go test ./...
task lint     # gofmt check + golangci-lint + yamllint
task license  # verify MIT headers (golic)
```

## ⚖️ License

MIT (c) 2026 Shane
