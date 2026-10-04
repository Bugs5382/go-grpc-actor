package grpcactor_test

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
	"errors"
	"sync"
	"testing"

	"github.com/Bugs5382/go-apperr"
	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const forgedRoot = `{"v":1,"sub":"root"}`

func TestDefaultTrustsNothing(t *testing.T) {
	rec := newRecorder()
	lis := serve(t, nil, &hop{name: "edge", rec: rec})
	client := dialRaw(t, lis, nil)

	ctx := metadata.AppendToOutgoingContext(ctxTimeout(t), grpcactor.MetadataKey, forgedRoot)
	if _, err := client.Check(ctx, checkReq("forged")); err != nil {
		t.Fatal(err)
	}
	s := rec.get(t, "edge", "forged")
	if s.hasActor {
		t.Fatalf("default server accepted a client-supplied actor: %+v", s.actor)
	}
	if s.rawKey {
		t.Fatal("default server left the client-supplied actor metadata for the handler")
	}
}

func TestUntrustedCallerStripped(t *testing.T) {
	c := newChain(t)
	// The gateway is a real, authenticated workload, but C trusts only B.
	direct := dial(t, c.lisC, c.pki.clientCreds(c.gwCert, dnsSvcC))
	ctx := grpcactor.WithActor(ctxTimeout(t), grpcactor.Actor{Subject: userSubject, Impersonator: adminActing})
	if _, err := direct.Check(ctx, checkReq("skip-hop")); err != nil {
		t.Fatal(err)
	}
	s := c.rec.get(t, "C", "skip-hop")
	if s.hasActor || s.rawKey {
		t.Fatalf("C accepted or kept an actor from an untrusted caller: %+v raw=%v", s.actor, s.rawKey)
	}
}

func TestUntrustedCallerRejected(t *testing.T) {
	var mu sync.Mutex
	var events []grpcactor.Event
	observe := func(_ context.Context, e grpcactor.Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	c := newChain(t, grpcactor.WithRejectUntrusted(), grpcactor.WithObserver(observe))
	direct := dial(t, c.lisC, c.pki.clientCreds(c.gwCert, dnsSvcC))

	ctx := grpcactor.WithActor(ctxTimeout(t), grpcactor.Actor{Subject: userSubject})
	_, err := direct.Check(ctx, checkReq("skip-hop"))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want PermissionDenied", status.Code(err))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("observer saw %d events, want 1: %+v", len(events), events)
	}
	e := events[0]
	if e.Outcome != grpcactor.OutcomeRejected || e.FullMethod != "/grpc.health.v1.Health/Check" || e.Claimed.Subject != userSubject {
		t.Fatalf("event = %+v", e)
	}
	if !errors.Is(e.Err, grpcactor.ErrUntrustedCaller) {
		t.Fatalf("event error %v does not wrap ErrUntrustedCaller", e.Err)
	}
}

func TestRejectUntrustedStillServesCallsWithoutActor(t *testing.T) {
	rec := newRecorder()
	lis := serve(t, nil, &hop{name: "svc", rec: rec}, grpcactor.WithRejectUntrusted())
	if _, err := dialRaw(t, lis, nil).Check(ctxTimeout(t), checkReq("probe")); err != nil {
		t.Fatalf("a call with no actor metadata was refused: %v", err)
	}
}

func TestTrustedMalformedActorRejected(t *testing.T) {
	tests := []struct {
		name string
		vals []string
	}{
		{"not json", []string{"not json"}},
		{"unknown version", []string{`{"v":2,"sub":"user-1001"}`}},
		{"no subject", []string{`{"v":1,"imp":"admin-7"}`}},
		{"two values", []string{forgedRoot, forgedRoot}},
		{"oversized", []string{`{"v":1,"sub":"` + string(make([]byte, grpcactor.MaxWireLen)) + `"}`}},
	}
	trustAll := func(context.Context, string) bool { return true }
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lis := serve(t, nil, &hop{name: "svc", rec: newRecorder()}, grpcactor.WithTrust(trustAll))
			ctx := ctxTimeout(t)
			for _, v := range tc.vals {
				ctx = metadata.AppendToOutgoingContext(ctx, grpcactor.MetadataKey, v)
			}
			_, err := dialRaw(t, lis, nil).Check(ctx, checkReq("bad"))
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("status code = %v, want InvalidArgument", status.Code(err))
			}
		})
	}
}

func TestRequireActor(t *testing.T) {
	onlyCheck := func(m string) bool { return m == "/grpc.health.v1.Health/Check" }
	rec := newRecorder()
	lis := serve(t, nil, &hop{name: "svc", rec: rec}, grpcactor.WithRequireActor(onlyCheck))
	_, err := dialRaw(t, lis, nil).Check(ctxTimeout(t), checkReq("anon"))
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestRequireActorUntrustedIsUnauthenticated(t *testing.T) {
	lis := serve(t, nil, &hop{name: "svc", rec: newRecorder()},
		grpcactor.WithRequireActor(func(string) bool { return true }))
	ctx := metadata.AppendToOutgoingContext(ctxTimeout(t), grpcactor.MetadataKey, forgedRoot)
	_, err := dialRaw(t, lis, nil).Check(ctx, checkReq("forged"))
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status code = %v, want Unauthenticated", status.Code(err))
	}
}

// runUnary calls the server interceptor directly, so the test can inspect the
// error before gRPC turns it into a status.
func runUnary(ctx context.Context, opts ...grpcactor.ServerOption) (context.Context, error) {
	var got context.Context
	handler := func(ctx context.Context, _ any) (any, error) {
		got = ctx
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/pkg.Svc/Method"}
	_, err := grpcactor.UnaryServerInterceptor(opts...)(ctx, nil, info, handler)
	return got, err
}

func TestServerErrorsCarryApperrCodes(t *testing.T) {
	codesOpt := grpcactor.WithCodes(grpcactor.Codes{Missing: 4011, Untrusted: 4012, Invalid: 4013})
	trustAll := grpcactor.WithTrust(func(context.Context, string) bool { return true })
	require := grpcactor.WithRequireActor(func(string) bool { return true })

	tests := []struct {
		name     string
		md       string
		opts     []grpcactor.ServerOption
		code     int
		sentinel error
		status   codes.Code
	}{
		{"missing", "", []grpcactor.ServerOption{codesOpt, require}, 4011, grpcactor.ErrNoActor, codes.Unauthenticated},
		{"untrusted", forgedRoot, []grpcactor.ServerOption{codesOpt, grpcactor.WithRejectUntrusted()}, 4012, grpcactor.ErrUntrustedCaller, codes.PermissionDenied},
		{"invalid", "junk", []grpcactor.ServerOption{codesOpt, trustAll}, 4013, grpcactor.ErrInvalidActor, codes.InvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.md != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(grpcactor.MetadataKey, tc.md))
			}
			_, err := runUnary(ctx, tc.opts...)
			if got, ok := apperr.Code(err); !ok || got != tc.code {
				t.Fatalf("apperr.Code = %d, %v; want %d", got, ok, tc.code)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v does not wrap %v", err, tc.sentinel)
			}
			if status.Code(err) != tc.status {
				t.Fatalf("status = %v, want %v", status.Code(err), tc.status)
			}
		})
	}
}

func TestServerKeepsOtherIncomingMetadata(t *testing.T) {
	in := metadata.Pairs(grpcactor.MetadataKey, forgedRoot, "x-request-id", "req-1")
	got, err := runUnary(metadata.NewIncomingContext(context.Background(), in))
	if err != nil {
		t.Fatal(err)
	}
	md, _ := metadata.FromIncomingContext(got)
	if len(md.Get(grpcactor.MetadataKey)) != 0 || len(md.Get("x-request-id")) != 1 {
		t.Fatalf("handler metadata = %v", md)
	}
	if len(in.Get(grpcactor.MetadataKey)) != 1 {
		t.Fatal("the interceptor modified the caller's metadata map in place")
	}
}

func TestObserverSeesAcceptedAndStripped(t *testing.T) {
	var events []grpcactor.Event
	observe := func(_ context.Context, e grpcactor.Event) { events = append(events, e) }
	in := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(grpcactor.MetadataKey, `{"v":1,"sub":"user-1001","imp":"admin-7"}`))

	if _, err := runUnary(in, grpcactor.WithObserver(observe)); err != nil {
		t.Fatal(err)
	}
	trust := grpcactor.WithTrust(func(context.Context, string) bool { return true })
	if _, err := runUnary(in, trust, grpcactor.WithObserver(observe)); err != nil {
		t.Fatal(err)
	}
	if _, err := runUnary(context.Background(), grpcactor.WithObserver(observe)); err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("observer saw %d events, want 2 (no event for a call with no actor): %+v", len(events), events)
	}
	if events[0].Outcome != grpcactor.OutcomeStripped || events[0].Claimed.Impersonator != adminActing {
		t.Fatalf("first event = %+v, want stripped with the claimed actor", events[0])
	}
	if events[1].Outcome != grpcactor.OutcomeAccepted || events[1].Claimed.Subject != userSubject {
		t.Fatalf("second event = %+v, want accepted", events[1])
	}
	for _, e := range events {
		if e.Outcome.String() == "" {
			t.Fatalf("outcome %d has no name", e.Outcome)
		}
	}
}
