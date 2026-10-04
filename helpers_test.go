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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const (
	idGateway  = "spiffe://example.org/ns/apps/sa/gateway"
	idSvcA     = "spiffe://example.org/ns/apps/sa/svc-a"
	idSvcB     = "spiffe://example.org/ns/apps/sa/svc-b"
	idSvcC     = "spiffe://example.org/ns/apps/sa/svc-c"
	idIntruder = "spiffe://example.org/ns/apps/sa/intruder"

	dnsSvcA = "svc-a.example.org"
	dnsSvcB = "svc-b.example.org"
	dnsSvcC = "svc-c.example.org"

	userSubject = "user-1001"
	adminActing = "admin-7"
)

// testPKI is a throwaway CA that issues certificates carrying a SPIFFE URI SAN
// and DNS SANs, usable for both ends of a mutual TLS connection.
type testPKI struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newPKI(t *testing.T) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testPKI{cert: cert, key: key, pool: pool}
}

var serial struct {
	sync.Mutex
	n int64
}

func nextSerial() *big.Int {
	serial.Lock()
	defer serial.Unlock()
	serial.n++
	return big.NewInt(serial.n + 1)
}

func leafTemplate(t *testing.T, spiffeID string, dnsNames ...string) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: "workload"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     dnsNames,
	}
	if spiffeID != "" {
		u, err := url.Parse(spiffeID)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	return tmpl
}

// issue returns a CA-signed certificate for spiffeID and dnsNames.
func (p *testPKI) issue(t *testing.T, spiffeID string, dnsNames ...string) tls.Certificate {
	t.Helper()
	return p.sign(t, leafTemplate(t, spiffeID, dnsNames...), p.cert, p.key)
}

// selfSigned returns a certificate no CA vouches for, claiming spiffeID.
func selfSigned(t *testing.T, spiffeID string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := leafTemplate(t, spiffeID)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func (p *testPKI) sign(t *testing.T, tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func (p *testPKI) serverCreds(cert tls.Certificate) credentials.TransportCredentials {
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    p.pool,
		MinVersion:   tls.VersionTLS13,
	})
}

func (p *testPKI) clientCreds(cert tls.Certificate, serverName string) credentials.TransportCredentials {
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      p.pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	})
}

// seen is what one hop observed for one call.
type seen struct {
	actor    grpcactor.Actor
	hasActor bool
	rawKey   bool
}

// recorder collects what each hop saw, keyed by hop name and the request's
// Service field, which the tests use as a call id.
type recorder struct {
	mu   sync.Mutex
	seen map[string]seen
}

func newRecorder() *recorder { return &recorder{seen: map[string]seen{}} }

func (r *recorder) record(ctx context.Context, hop, call string) {
	a, ok := grpcactor.FromContext(ctx)
	md, _ := metadata.FromIncomingContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[hop+"/"+call] = seen{actor: a, hasActor: ok, rawKey: len(md.Get(grpcactor.MetadataKey)) > 0}
}

func (r *recorder) get(t *testing.T, hop, call string) seen {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.seen[hop+"/"+call]
	if !ok {
		t.Fatalf("hop %s never saw call %s", hop, call)
	}
	return s
}

// hop is a health service that records what it saw and, when next is set,
// calls the next hop with the context it was handed, the way a real service
// handler does.
type hop struct {
	healthpb.UnimplementedHealthServer
	name string
	next healthpb.HealthClient
	rec  *recorder
}

func (h *hop) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	h.rec.record(ctx, h.name, req.GetService())
	if h.next != nil {
		return h.next.Check(ctx, req)
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (h *hop) Watch(req *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	ctx := stream.Context()
	h.rec.record(ctx, h.name, req.GetService())
	if h.next != nil {
		down, err := h.next.Watch(ctx, req)
		if err != nil {
			return err
		}
		resp, err := down.Recv()
		if err != nil {
			return err
		}
		return stream.Send(resp)
	}
	return stream.Send(&healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING})
}

// serve starts impl on an in-memory listener with the actor server
// interceptors, and returns the listener.
func serve(t *testing.T, creds credentials.TransportCredentials, impl healthpb.HealthServer, opts ...grpcactor.ServerOption) *bufconn.Listener {
	t.Helper()
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(grpcactor.UnaryServerInterceptor(opts...)),
		grpc.ChainStreamInterceptor(grpcactor.StreamServerInterceptor(opts...)),
	)
	healthpb.RegisterHealthServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis
}

// dial connects to lis with the actor client interceptors.
func dial(t *testing.T, lis *bufconn.Listener, creds credentials.TransportCredentials) healthpb.HealthClient {
	t.Helper()
	return dialWith(t, lis, creds,
		grpc.WithChainUnaryInterceptor(grpcactor.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(grpcactor.StreamClientInterceptor()),
	)
}

// dialRaw connects to lis without the actor interceptors, so a test can put
// whatever metadata it likes on the wire.
func dialRaw(t *testing.T, lis *bufconn.Listener, creds credentials.TransportCredentials) healthpb.HealthClient {
	t.Helper()
	return dialWith(t, lis, creds)
}

func dialWith(t *testing.T, lis *bufconn.Listener, creds credentials.TransportCredentials, extra ...grpc.DialOption) healthpb.HealthClient {
	t.Helper()
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
	}, extra...)
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return healthpb.NewHealthClient(conn)
}

// chain is a gateway -> A -> B -> C mesh over mutual TLS. Each service trusts
// actor metadata only from the service directly in front of it.
type chain struct {
	pki     *testPKI
	rec     *recorder
	gateway healthpb.HealthClient // the gateway's client to A
	lisC    *bufconn.Listener
	gwCert  tls.Certificate
}

func newChain(t *testing.T, extra ...grpcactor.ServerOption) *chain {
	t.Helper()
	pki := newPKI(t)
	rec := newRecorder()
	certA := pki.issue(t, idSvcA, dnsSvcA)
	certB := pki.issue(t, idSvcB, dnsSvcB)
	certC := pki.issue(t, idSvcC, dnsSvcC)
	gwCert := pki.issue(t, idGateway)

	lisC := serve(t, pki.serverCreds(certC), &hop{name: "C", rec: rec},
		append([]grpcactor.ServerOption{grpcactor.WithTrust(grpcactor.TrustSPIFFEIDs(idSvcB))}, extra...)...)
	toC := dial(t, lisC, pki.clientCreds(certB, dnsSvcC))

	lisB := serve(t, pki.serverCreds(certB), &hop{name: "B", next: toC, rec: rec},
		append([]grpcactor.ServerOption{grpcactor.WithTrust(grpcactor.TrustSPIFFEIDs(idSvcA))}, extra...)...)
	toB := dial(t, lisB, pki.clientCreds(certA, dnsSvcB))

	lisA := serve(t, pki.serverCreds(certA), &hop{name: "A", next: toB, rec: rec},
		append([]grpcactor.ServerOption{grpcactor.WithTrust(grpcactor.TrustSPIFFEIDs(idGateway))}, extra...)...)
	toA := dial(t, lisA, pki.clientCreds(gwCert, dnsSvcA))

	return &chain{pki: pki, rec: rec, gateway: toA, lisC: lisC, gwCert: gwCert}
}

func ctxTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func checkReq(call string) *healthpb.HealthCheckRequest {
	return &healthpb.HealthCheckRequest{Service: call}
}
