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
	"crypto/x509"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// TrustFunc decides whether the caller of fullMethod is an authenticated
// internal service allowed to forward an actor. It must answer from what the
// transport or an earlier authentication interceptor proved (a verified mTLS
// peer, a verified workload token), never from anything else the caller sent.
type TrustFunc func(ctx context.Context, fullMethod string) bool

// TrustNone trusts no caller. It is the server default, so a service that
// forgets to configure trust fails closed.
func TrustNone(context.Context, string) bool { return false }

// TrustSPIFFEIDs trusts a caller whose verified mTLS client certificate carries
// exactly one URI SAN, a spiffe:// ID equal to one of ids. The server must
// verify client certificates (tls.RequireAndVerifyClientCert); a certificate
// the handshake did not verify is never trusted.
func TrustSPIFFEIDs(ids ...string) TrustFunc {
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	return func(ctx context.Context, _ string) bool {
		leaf, ok := verifiedLeaf(ctx)
		if !ok || len(leaf.URIs) != 1 {
			return false
		}
		u := leaf.URIs[0]
		return u.Scheme == "spiffe" && allowed[u.String()]
	}
}

// TrustDNSNames trusts a caller whose verified mTLS client certificate carries
// a DNS SAN equal, ignoring case, to one of names. Matching is exact: a
// wildcard SAN matches only the same wildcard in names.
func TrustDNSNames(names ...string) TrustFunc {
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		allowed[strings.ToLower(n)] = true
	}
	return func(ctx context.Context, _ string) bool {
		leaf, ok := verifiedLeaf(ctx)
		if !ok {
			return false
		}
		for _, n := range leaf.DNSNames {
			if allowed[strings.ToLower(n)] {
				return true
			}
		}
		return false
	}
}

// AnyOf trusts a caller any of fns trusts. With no fns it trusts nobody.
func AnyOf(fns ...TrustFunc) TrustFunc {
	return func(ctx context.Context, fullMethod string) bool {
		for _, f := range fns {
			if f(ctx, fullMethod) {
				return true
			}
		}
		return false
	}
}

// ForMethods applies trust only to the listed full method names
// ("/pkg.Service/Method") and trusts nobody on any other method. It builds a
// per-method allow-list: combine one per caller group with AnyOf.
func ForMethods(trust TrustFunc, methods ...string) TrustFunc {
	listed := make(map[string]bool, len(methods))
	for _, m := range methods {
		listed[m] = true
	}
	return func(ctx context.Context, fullMethod string) bool {
		return listed[fullMethod] && trust(ctx, fullMethod)
	}
}

func verifiedLeaf(ctx context.Context) (*x509.Certificate, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, false
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return nil, false
	}
	return info.State.PeerCertificates[0], true
}
