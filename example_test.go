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
	"fmt"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc"
)

// An internal service trusts actor metadata only from the services that call
// it with a verified mTLS client certificate, and forwards the actor on every
// call it makes.
func Example() {
	trust := grpcactor.TrustSPIFFEIDs(
		"spiffe://example.org/ns/apps/sa/gateway",
		"spiffe://example.org/ns/apps/sa/workflow",
	)
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcactor.UnaryServerInterceptor(grpcactor.WithTrust(trust))),
		grpc.ChainStreamInterceptor(grpcactor.StreamServerInterceptor(grpcactor.WithTrust(trust))),
	)
	defer server.Stop()

	conn, err := grpc.NewClient("dns:///svc-b.example.org:443",
		// grpc.WithTransportCredentials(the workload's mTLS credentials),
		grpc.WithChainUnaryInterceptor(grpcactor.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(grpcactor.StreamClientInterceptor()),
	)
	if err == nil {
		_ = conn.Close()
	}
}

// The edge sets the actor once, after it has authenticated the user; during
// act-as the real admin rides along as the impersonator.
func ExampleWithActor() {
	ctx := grpcactor.WithActor(context.Background(), grpcactor.Actor{
		Subject:      "user-1001",
		Impersonator: "admin-7",
	})
	a, _ := grpcactor.FromContext(ctx)
	fmt.Println(a.Subject, a.RealUser(), a.Impersonated())
	// Output: user-1001 admin-7 true
}

// A handler that cannot run without a user asks for one; the error carries the
// service's own go-apperr code and an Unauthenticated gRPC status.
func ExampleRequire() {
	const codeNoActor = 4011
	_, err := grpcactor.Require(context.Background(), codeNoActor)
	fmt.Println(err)
	// Output: code 4011: grpcactor: no actor in the request
}
