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
	"errors"

	"github.com/Bugs5382/go-apperr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// ErrNoActor means a method that needs an actor got none. It converts to
	// an Unauthenticated gRPC status.
	ErrNoActor = errors.New("grpcactor: no actor in the request")
	// ErrUntrustedCaller means a caller the trust check refused sent actor
	// metadata, and the server was set to refuse rather than strip it. It
	// converts to a PermissionDenied gRPC status.
	ErrUntrustedCaller = errors.New("grpcactor: actor sent by an untrusted caller")
	// ErrInvalidActor means the actor is malformed: bad wire data from a
	// trusted caller, or an Actor that fails Validate. It converts to an
	// InvalidArgument gRPC status.
	ErrInvalidActor = errors.New("grpcactor: invalid actor")
)

// Codes are the go-apperr codes the server interceptors attach to the errors
// they return, one per sentinel. They are the consumer's own codes; a zero
// field leaves that error without one.
type Codes struct {
	Missing   int
	Untrusted int
	Invalid   int
}

// refusal is an error that knows its gRPC status, so it survives being wrapped
// by apperr.Coded and still reaches the caller with the right code.
type refusal struct {
	code codes.Code
	err  error
}

func (r *refusal) Error() string              { return r.err.Error() }
func (r *refusal) Unwrap() error              { return r.err }
func (r *refusal) GRPCStatus() *status.Status { return status.New(r.code, r.err.Error()) }

func refuse(err error) *refusal {
	switch {
	case errors.Is(err, ErrNoActor):
		return &refusal{code: codes.Unauthenticated, err: err}
	case errors.Is(err, ErrUntrustedCaller):
		return &refusal{code: codes.PermissionDenied, err: err}
	default:
		return &refusal{code: codes.InvalidArgument, err: err}
	}
}

func coded(code int, r *refusal) error {
	if code == 0 {
		return r
	}
	return apperr.Coded(code, r)
}
