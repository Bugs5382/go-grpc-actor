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
	"encoding/json"
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestThreeHopsPreserveSubjectAndImpersonator(t *testing.T) {
	c := newChain(t)
	want := grpcactor.Actor{Subject: userSubject, Impersonator: adminActing, Tenant: "tenant-1", Session: "sess-1"}
	ctx := grpcactor.WithActor(ctxTimeout(t), want)

	if _, err := c.gateway.Check(ctx, checkReq("act-as")); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"A", "B", "C"} {
		s := c.rec.get(t, h, "act-as")
		if !s.hasActor || s.actor != want {
			t.Errorf("hop %s saw %+v (ok=%v), want %+v", h, s.actor, s.hasActor, want)
		}
		if s.rawKey {
			t.Errorf("hop %s handler can still read the raw actor metadata", h)
		}
	}
}

func TestThreeHopsWithoutImpersonator(t *testing.T) {
	c := newChain(t)
	want := grpcactor.Actor{Subject: userSubject}
	if _, err := c.gateway.Check(grpcactor.WithActor(ctxTimeout(t), want), checkReq("plain")); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"A", "B", "C"} {
		s := c.rec.get(t, h, "plain")
		if s.actor != want || s.actor.Impersonated() {
			t.Errorf("hop %s saw %+v, want %+v with no impersonator", h, s.actor, want)
		}
	}
}

func TestNoActorInContextReachesNoHop(t *testing.T) {
	c := newChain(t)
	if _, err := c.gateway.Check(ctxTimeout(t), checkReq("system")); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"A", "B", "C"} {
		if s := c.rec.get(t, h, "system"); s.hasActor {
			t.Errorf("hop %s saw an actor on a call that carried none: %+v", h, s.actor)
		}
	}
}

// captureOutgoing runs the unary client interceptor and returns the outgoing
// metadata the invoker was handed.
func captureOutgoing(t *testing.T, ctx context.Context) (metadata.MD, error) {
	t.Helper()
	var got metadata.MD
	invoker := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		got, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}
	err := grpcactor.UnaryClientInterceptor()(ctx, "/pkg.Svc/Method", nil, nil, nil, invoker)
	return got, err
}

func TestClientWireFormat(t *testing.T) {
	ctx := grpcactor.WithActor(context.Background(), grpcactor.Actor{Subject: userSubject, Impersonator: adminActing})
	md, err := captureOutgoing(t, ctx)
	if err != nil {
		t.Fatal(err)
	}
	vals := md.Get(grpcactor.MetadataKey)
	if len(vals) != 1 {
		t.Fatalf("outgoing %s has %d values, want 1", grpcactor.MetadataKey, len(vals))
	}
	var wire map[string]any
	if err := json.Unmarshal([]byte(vals[0]), &wire); err != nil {
		t.Fatalf("wire value is not JSON: %v", err)
	}
	want := map[string]any{"v": float64(1), "sub": userSubject, "imp": adminActing}
	if len(wire) != len(want) {
		t.Fatalf("wire = %v, want %v", wire, want)
	}
	for k, v := range want {
		if wire[k] != v {
			t.Fatalf("wire[%q] = %v, want %v", k, wire[k], v)
		}
	}
}

func TestClientReplacesHandWrittenActorMetadata(t *testing.T) {
	forged := metadata.AppendToOutgoingContext(context.Background(), grpcactor.MetadataKey, `{"v":1,"sub":"root"}`)

	md, err := captureOutgoing(t, forged)
	if err != nil {
		t.Fatal(err)
	}
	if vals := md.Get(grpcactor.MetadataKey); len(vals) != 0 {
		t.Fatalf("no actor in context, but %v went on the wire", vals)
	}

	md, err = captureOutgoing(t, grpcactor.WithActor(forged, grpcactor.Actor{Subject: userSubject}))
	if err != nil {
		t.Fatal(err)
	}
	if vals := md.Get(grpcactor.MetadataKey); len(vals) != 1 {
		t.Fatalf("want exactly the context actor on the wire, got %v", vals)
	}
}

func TestClientRefusesInvalidActor(t *testing.T) {
	ctx := grpcactor.WithActor(context.Background(), grpcactor.Actor{Impersonator: adminActing})
	_, err := captureOutgoing(t, ctx)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status code = %v, want InvalidArgument", status.Code(err))
	}
}
