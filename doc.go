// Package grpcactor carries the signed-in user, and during act-as the real
// admin behind them, across gRPC service hops, and trusts it only from
// authenticated internal callers.
//
// # The actor
//
// An Actor names who a request is for. Subject is the effective user, the one
// authorization evaluates as. Impersonator is the admin acting as Subject, and
// is empty when nobody is impersonating; RealUser returns whichever person is
// actually at the keyboard, for audit. Tenant and Session ride along as they
// are. The edge sets the actor once, with WithActor, after it has
// authenticated the user; everything after that reads it with FromContext.
//
// # Hops
//
// The client interceptors (UnaryClientInterceptor, StreamClientInterceptor)
// write the context actor to outgoing metadata under MetadataKey. The server
// interceptors (UnaryServerInterceptor, StreamServerInterceptor) read it back
// into the handler's context. A handler that calls the next service with the
// context it was given forwards the same actor, impersonator included, with no
// code of its own. Edge-set and forwarded actors are the same value in the
// same context key, so no hop can forget one half of it.
//
// Only the context actor goes out. Actor metadata a caller wrote by hand is
// replaced, or removed when the context has no actor, so a system call that
// carries no user sends nothing.
//
// # Trust
//
// A server accepts the actor only when its TrustFunc says the caller is an
// authenticated internal service allowed to forward one. The default is
// TrustNone: a server that is not told whom to trust trusts nobody. Actor
// metadata is always removed from the incoming metadata the handler sees, so
// a client-supplied header can never be read by mistake. From an untrusted
// caller it is dropped (the call is served without an actor) or, with
// WithRejectUntrusted, refused with PermissionDenied. Edge services that face
// clients strip; internal services reject, so an attempt is audited.
//
// TrustSPIFFEIDs and TrustDNSNames trust an mTLS peer by its SPIFFE ID or a
// DNS SAN, and only when the handshake verified the client certificate.
// Services that authenticate callers with a workload token instead (a
// projected service-account token checked against the cluster's JWKS) write a
// TrustFunc that reads the verified caller from the context their auth
// interceptor fills, and install this package's interceptor after that one.
// ForMethods and AnyOf build per-method allow-lists, so only the callers that
// may pass an end-user actor to a method can do so.
//
// The actor is not signed. A signature with a shared key would add a shared
// secret between services, and the actor's integrity already rests on the
// authenticated channel: the trusted caller is the one vouching for it, and an
// untrusted caller's actor is never read. An attacker who can impersonate a
// trusted workload could sign whatever it liked, too.
//
// # Errors
//
// Refusals wrap ErrNoActor, ErrUntrustedCaller or ErrInvalidActor and convert
// to Unauthenticated, PermissionDenied and InvalidArgument gRPC statuses.
// Require and WithCodes attach the service's own go-apperr codes to them.
// WithObserver reports every decision for audit or logging.
package grpcactor

/*
MIT License

Copyright (c) 2026 Shane

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
*/
