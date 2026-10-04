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
	"fmt"
	"unicode"
	"unicode/utf8"
)

// MaxFieldLen is the longest value, in bytes, any Actor field may hold.
const MaxFieldLen = 256

// Actor is who a request is for. Subject is the effective user: authorization
// evaluates as Subject. During act-as, Impersonator is the real admin doing the
// work, kept so every hop can attribute the action to them; it is empty
// otherwise. Tenant and Session are optional and carried as they are.
//
// Roles and other grants are deliberately absent. A callee resolves what the
// subject may do from its own data, so a forwarded actor can never widen a
// caller's authority.
type Actor struct {
	Subject      string
	Impersonator string
	Tenant       string
	Session      string
}

// Impersonated reports whether an admin is acting as Subject.
func (a Actor) Impersonated() bool { return a.Impersonator != "" }

// RealUser returns the person actually at the keyboard: the impersonator
// during act-as, otherwise the subject. Audit records should name this user.
func (a Actor) RealUser() string {
	if a.Impersonated() {
		return a.Impersonator
	}
	return a.Subject
}

// Validate reports whether a is fit to put on the wire. Subject is required,
// an admin cannot impersonate themselves, and every field must be valid UTF-8
// of at most MaxFieldLen bytes with no control characters. The error wraps
// ErrInvalidActor.
func (a Actor) Validate() error {
	if a.Subject == "" {
		return fmt.Errorf("%w: subject is empty", ErrInvalidActor)
	}
	if a.Impersonator == a.Subject {
		return fmt.Errorf("%w: impersonator is the subject", ErrInvalidActor)
	}
	for _, f := range []struct{ name, value string }{
		{"subject", a.Subject},
		{"impersonator", a.Impersonator},
		{"tenant", a.Tenant},
		{"session", a.Session},
	} {
		if err := checkField(f.value); err != nil {
			return fmt.Errorf("%w: %s %s", ErrInvalidActor, f.name, err)
		}
	}
	return nil
}

func checkField(v string) error {
	if len(v) > MaxFieldLen {
		return fmt.Errorf("is longer than %d bytes", MaxFieldLen)
	}
	if !utf8.ValidString(v) {
		return fmt.Errorf("is not valid UTF-8")
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("contains a control character")
		}
	}
	return nil
}

type actorKey struct{}

// WithActor returns a copy of ctx carrying a. The edge calls it once it has
// authenticated the user; the server interceptors call it for a trusted hop.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// FromContext returns the actor in ctx, if any.
func FromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// Require returns the actor in ctx, or, when there is none, an error that
// wraps ErrNoActor, carries code as its go-apperr code and converts to an
// Unauthenticated gRPC status. code is the consumer's own: register it in the
// service's apperr registry. A zero code leaves the error uncoded.
func Require(ctx context.Context, code int) (Actor, error) {
	if a, ok := FromContext(ctx); ok {
		return a, nil
	}
	return Actor{}, coded(code, refuse(ErrNoActor))
}
