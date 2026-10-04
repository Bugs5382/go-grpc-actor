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
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

const method = "/pkg.Svc/Method"

type plainInfo struct{}

func (plainInfo) AuthType() string { return "insecure" }

func peerCtx(info credentials.AuthInfo) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{}, AuthInfo: info})
}

func tlsPeer(leaf *x509.Certificate, verified bool) context.Context {
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	if verified {
		state.VerifiedChains = [][]*x509.Certificate{{leaf}}
	}
	return peerCtx(credentials.TLSInfo{State: state})
}

func leafWith(t *testing.T, uris []string, dns ...string) *x509.Certificate {
	t.Helper()
	c := &x509.Certificate{DNSNames: dns}
	for _, s := range uris {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		c.URIs = append(c.URIs, u)
	}
	return c
}

func TestTrustNone(t *testing.T) {
	if grpcactor.TrustNone(tlsPeer(leafWith(t, []string{idSvcA}), true), method) {
		t.Fatal("TrustNone trusted a verified peer")
	}
}

func TestTrustSPIFFEIDs(t *testing.T) {
	trust := grpcactor.TrustSPIFFEIDs(idSvcA, idSvcB)
	tests := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"listed id", tlsPeer(leafWith(t, []string{idSvcA}), true), true},
		{"second listed id", tlsPeer(leafWith(t, []string{idSvcB}), true), true},
		{"unlisted id", tlsPeer(leafWith(t, []string{idIntruder}), true), false},
		{"listed id, chain not verified", tlsPeer(leafWith(t, []string{idSvcA}), false), false},
		{"two URI SANs", tlsPeer(leafWith(t, []string{idIntruder, idSvcA}), true), false},
		{"not a spiffe uri", tlsPeer(leafWith(t, []string{"https://example.org/ns/apps/sa/svc-a"}), true), false},
		{"dns name only", tlsPeer(leafWith(t, nil, dnsSvcA), true), false},
		{"no peer", context.Background(), false},
		{"plaintext peer", peerCtx(plainInfo{}), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trust(tc.ctx, method); got != tc.want {
				t.Fatalf("trust = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTrustDNSNames(t *testing.T) {
	trust := grpcactor.TrustDNSNames(dnsSvcA)
	tests := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"listed name", tlsPeer(leafWith(t, nil, "other.example.org", dnsSvcA), true), true},
		{"listed name, other case", tlsPeer(leafWith(t, nil, "SVC-A.Example.org"), true), true},
		{"unlisted name", tlsPeer(leafWith(t, nil, dnsSvcB), true), false},
		{"listed name, chain not verified", tlsPeer(leafWith(t, nil, dnsSvcA), false), false},
		{"spiffe id only", tlsPeer(leafWith(t, []string{idSvcA}), true), false},
		{"no peer", context.Background(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trust(tc.ctx, method); got != tc.want {
				t.Fatalf("trust = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTrustAnyOf(t *testing.T) {
	trust := grpcactor.AnyOf(grpcactor.TrustSPIFFEIDs(idSvcA), grpcactor.TrustDNSNames(dnsSvcB))
	if !trust(tlsPeer(leafWith(t, []string{idSvcA}), true), method) {
		t.Fatal("AnyOf refused a peer the first check trusts")
	}
	if !trust(tlsPeer(leafWith(t, nil, dnsSvcB), true), method) {
		t.Fatal("AnyOf refused a peer the second check trusts")
	}
	if trust(tlsPeer(leafWith(t, []string{idIntruder}), true), method) {
		t.Fatal("AnyOf trusted a peer neither check trusts")
	}
	if grpcactor.AnyOf()(tlsPeer(leafWith(t, []string{idSvcA}), true), method) {
		t.Fatal("an empty AnyOf trusted a peer")
	}
}

func TestForMethods(t *testing.T) {
	trust := grpcactor.ForMethods(grpcactor.TrustSPIFFEIDs(idSvcA), "/pkg.Svc/Reveal")
	ctx := tlsPeer(leafWith(t, []string{idSvcA}), true)
	if !trust(ctx, "/pkg.Svc/Reveal") {
		t.Fatal("ForMethods refused a listed method")
	}
	if trust(ctx, "/pkg.Svc/Other") {
		t.Fatal("ForMethods trusted an unlisted method")
	}
}

func TestUnverifiedClientCertNotTrusted(t *testing.T) {
	pki := newPKI(t)
	rec := newRecorder()
	// A server that asks for a client certificate but does not verify it: the
	// trust helper must not take the certificate's word for who it is.
	lax := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pki.issue(t, idSvcC, dnsSvcC)},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS13,
	})
	lis := serve(t, lax, &hop{name: "C", rec: rec}, grpcactor.WithTrust(grpcactor.TrustSPIFFEIDs(idSvcB)))
	forger := dial(t, lis, pki.clientCreds(selfSigned(t, idSvcB), dnsSvcC))

	ctx := grpcactor.WithActor(ctxTimeout(t), grpcactor.Actor{Subject: userSubject, Impersonator: adminActing})
	if _, err := forger.Check(ctx, checkReq("forged-cert")); err != nil {
		t.Fatal(err)
	}
	if s := rec.get(t, "C", "forged-cert"); s.hasActor {
		t.Fatalf("an unverified certificate claiming a trusted id was trusted: %+v", s.actor)
	}
}
