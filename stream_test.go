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
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestStreamThreeHops(t *testing.T) {
	c := newChain(t)
	want := grpcactor.Actor{Subject: userSubject, Impersonator: adminActing}
	stream, err := c.gateway.Watch(grpcactor.WithActor(ctxTimeout(t), want), checkReq("watch"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"A", "B", "C"} {
		s := c.rec.get(t, h, "watch")
		if !s.hasActor || s.actor != want || s.rawKey {
			t.Errorf("hop %s saw %+v (ok=%v raw=%v), want %+v", h, s.actor, s.hasActor, s.rawKey, want)
		}
	}
}

func TestStreamUntrustedStripped(t *testing.T) {
	rec := newRecorder()
	lis := serve(t, nil, &hop{name: "edge", rec: rec})
	ctx := metadata.AppendToOutgoingContext(ctxTimeout(t), grpcactor.MetadataKey, forgedRoot)
	stream, err := dialRaw(t, lis, nil).Watch(ctx, checkReq("forged-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if s := rec.get(t, "edge", "forged-stream"); s.hasActor || s.rawKey {
		t.Fatalf("stream accepted or kept a client-supplied actor: %+v raw=%v", s.actor, s.rawKey)
	}
}

func TestStreamRequireActor(t *testing.T) {
	lis := serve(t, nil, &hop{name: "svc", rec: newRecorder()},
		grpcactor.WithRequireActor(func(string) bool { return true }))
	stream, err := dialRaw(t, lis, nil).Watch(ctxTimeout(t), checkReq("anon-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestStreamRejectUntrusted(t *testing.T) {
	lis := serve(t, nil, &hop{name: "svc", rec: newRecorder()}, grpcactor.WithRejectUntrusted())
	ctx := metadata.AppendToOutgoingContext(ctxTimeout(t), grpcactor.MetadataKey, forgedRoot)
	stream, err := dialRaw(t, lis, nil).Watch(ctx, checkReq("forged-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want PermissionDenied", status.Code(err))
	}
}
