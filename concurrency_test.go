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
	"fmt"
	"sync"
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
)

// TestConcurrentCallsKeepTheirOwnActor runs many unary and streaming calls at
// once over the same connections, each with its own actor, and checks that no
// hop ever sees another call's actor. Run it with -race.
func TestConcurrentCallsKeepTheirOwnActor(t *testing.T) {
	c := newChain(t)
	const calls = 64

	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := fmt.Sprintf("call-%d", i)
			a := grpcactor.Actor{Subject: "user-" + call}
			if i%3 == 0 {
				a.Impersonator = "admin-" + call
			}
			ctx := grpcactor.WithActor(ctxTimeout(t), a)
			if i%2 == 0 {
				if _, err := c.gateway.Check(ctx, checkReq(call)); err != nil {
					errs <- err
				}
				return
			}
			stream, err := c.gateway.Watch(ctx, checkReq(call))
			if err != nil {
				errs <- err
				return
			}
			if _, err := stream.Recv(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	for i := range calls {
		call := fmt.Sprintf("call-%d", i)
		want := grpcactor.Actor{Subject: "user-" + call}
		if i%3 == 0 {
			want.Impersonator = "admin-" + call
		}
		for _, h := range []string{"A", "B", "C"} {
			if s := c.rec.get(t, h, call); s.actor != want {
				t.Errorf("hop %s, %s: saw %+v, want %+v", h, call, s.actor, want)
			}
		}
	}
}
