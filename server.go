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

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Outcome is what a server interceptor did with the actor metadata on a call.
type Outcome int

const (
	// OutcomeAccepted: a trusted caller sent a valid actor, now in the context.
	OutcomeAccepted Outcome = iota + 1
	// OutcomeStripped: an untrusted caller sent an actor and it was dropped.
	OutcomeStripped
	// OutcomeRejected: the call was refused (untrusted with WithRejectUntrusted,
	// a malformed actor, or a required actor missing).
	OutcomeRejected
)

// String returns "accepted", "stripped" or "rejected".
func (o Outcome) String() string {
	switch o {
	case OutcomeAccepted:
		return "accepted"
	case OutcomeStripped:
		return "stripped"
	case OutcomeRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// Event describes one decision, for audit and logging. Claimed is the actor
// the caller sent, decoded on a best-effort basis; for a stripped or rejected
// call it is unverified and must not be treated as who made the call. Err is
// set when the call was rejected.
type Event struct {
	FullMethod string
	Outcome    Outcome
	Claimed    Actor
	Err        error
}

// Observer receives an Event for every call that carried actor metadata and
// for every call refused for a missing actor. It runs on the request path, so
// keep it fast.
type Observer func(ctx context.Context, e Event)

// ServerOption configures the server interceptors.
type ServerOption func(*serverConfig)

type serverConfig struct {
	trust    TrustFunc
	reject   bool
	required func(fullMethod string) bool
	codes    Codes
	observe  Observer
}

// WithTrust sets the check that decides whether the caller may forward an
// actor. Without it the server trusts no caller (TrustNone).
func WithTrust(trust TrustFunc) ServerOption {
	return func(c *serverConfig) { c.trust = trust }
}

// WithRejectUntrusted refuses, with PermissionDenied, a call whose untrusted
// caller sent actor metadata, instead of stripping the metadata and serving
// the call as anonymous. Internal services should set it, so a caller that
// tries to pass an actor it may not pass is refused and audited rather than
// silently downgraded. Edge services that face clients leave it off.
func WithRejectUntrusted() ServerOption {
	return func(c *serverConfig) { c.reject = true }
}

// WithRequireActor refuses, with Unauthenticated, a call to a method match
// reports true for when no actor is in the context after the trust check.
func WithRequireActor(match func(fullMethod string) bool) ServerOption {
	return func(c *serverConfig) { c.required = match }
}

// WithCodes sets the go-apperr codes attached to the errors the interceptors
// return.
func WithCodes(codes Codes) ServerOption {
	return func(c *serverConfig) { c.codes = codes }
}

// WithObserver sets a callback for each decision, for audit or logging.
func WithObserver(observe Observer) ServerOption {
	return func(c *serverConfig) { c.observe = observe }
}

func newServerConfig(opts []ServerOption) *serverConfig {
	c := &serverConfig{trust: TrustNone}
	for _, o := range opts {
		o(c)
	}
	return c
}

// UnaryServerInterceptor reads the actor a caller sent into the handler's
// context, but only when the trust check passes. Actor metadata is always
// removed from the incoming metadata the handler sees, trusted or not, so
// FromContext is the only way to read it.
//
// Install it after the interceptor that authenticates the caller, when the
// trust check depends on what that interceptor puts in the context.
func UnaryServerInterceptor(opts ...ServerOption) grpc.UnaryServerInterceptor {
	c := newServerConfig(opts)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := c.admit(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor is UnaryServerInterceptor for streaming calls.
func StreamServerInterceptor(opts ...ServerOption) grpc.StreamServerInterceptor {
	c := newServerConfig(opts)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := c.admit(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &serverStream{ServerStream: ss, ctx: ctx})
	}
}

type serverStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStream) Context() context.Context { return s.ctx }

func (c *serverConfig) admit(ctx context.Context, method string) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if vals := md.Get(MetadataKey); len(vals) > 0 {
		md = md.Copy()
		md.Delete(MetadataKey)
		ctx = metadata.NewIncomingContext(ctx, md)

		claimed, decodeErr := decode(vals)
		switch {
		case !c.trust(ctx, method):
			if c.reject {
				return nil, c.refuse(ctx, method, claimed, ErrUntrustedCaller, c.codes.Untrusted)
			}
			c.notify(ctx, Event{FullMethod: method, Outcome: OutcomeStripped, Claimed: claimed})
		case decodeErr != nil:
			return nil, c.refuse(ctx, method, claimed, decodeErr, c.codes.Invalid)
		default:
			ctx = WithActor(ctx, claimed)
			c.notify(ctx, Event{FullMethod: method, Outcome: OutcomeAccepted, Claimed: claimed})
		}
	}
	if c.required != nil && c.required(method) {
		if _, ok := FromContext(ctx); !ok {
			return nil, c.refuse(ctx, method, Actor{}, ErrNoActor, c.codes.Missing)
		}
	}
	return ctx, nil
}

func (c *serverConfig) refuse(ctx context.Context, method string, claimed Actor, cause error, code int) error {
	err := coded(code, refuse(cause))
	c.notify(ctx, Event{FullMethod: method, Outcome: OutcomeRejected, Claimed: claimed, Err: err})
	return err
}

func (c *serverConfig) notify(ctx context.Context, e Event) {
	if c.observe != nil {
		c.observe(ctx, e)
	}
}
