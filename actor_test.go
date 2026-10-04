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
	"strings"
	"testing"

	"github.com/Bugs5382/go-apperr"
	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFromContextEmpty(t *testing.T) {
	if a, ok := grpcactor.FromContext(context.Background()); ok {
		t.Fatalf("empty context returned an actor: %+v", a)
	}
}

func TestWithActorRoundTrip(t *testing.T) {
	want := grpcactor.Actor{Subject: userSubject, Impersonator: adminActing, Tenant: "tenant-1", Session: "sess-1"}
	got, ok := grpcactor.FromContext(grpcactor.WithActor(context.Background(), want))
	if !ok || got != want {
		t.Fatalf("FromContext = %+v, %v; want %+v, true", got, ok, want)
	}
}

func TestActorRealUser(t *testing.T) {
	plain := grpcactor.Actor{Subject: userSubject}
	if plain.Impersonated() || plain.RealUser() != userSubject {
		t.Fatalf("plain actor: Impersonated=%v RealUser=%q", plain.Impersonated(), plain.RealUser())
	}
	actAs := grpcactor.Actor{Subject: userSubject, Impersonator: adminActing}
	if !actAs.Impersonated() || actAs.RealUser() != adminActing {
		t.Fatalf("act-as actor: Impersonated=%v RealUser=%q", actAs.Impersonated(), actAs.RealUser())
	}
}

func TestActorValidate(t *testing.T) {
	long := strings.Repeat("x", grpcactor.MaxFieldLen+1)
	tests := []struct {
		name  string
		actor grpcactor.Actor
		ok    bool
	}{
		{"subject only", grpcactor.Actor{Subject: userSubject}, true},
		{"all fields", grpcactor.Actor{Subject: userSubject, Impersonator: adminActing, Tenant: "t", Session: "s"}, true},
		{"no subject", grpcactor.Actor{Impersonator: adminActing}, false},
		{"impersonating self", grpcactor.Actor{Subject: userSubject, Impersonator: userSubject}, false},
		{"control character", grpcactor.Actor{Subject: "user\n1001"}, false},
		{"control character in tenant", grpcactor.Actor{Subject: userSubject, Tenant: "t\x00"}, false},
		{"field too long", grpcactor.Actor{Subject: long}, false},
		{"invalid utf-8", grpcactor.Actor{Subject: "user-\xff"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.actor.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, grpcactor.ErrInvalidActor) {
				t.Fatalf("Validate() = %v, want ErrInvalidActor", err)
			}
		})
	}
}

func TestRequireMissing(t *testing.T) {
	const code = 4011
	_, err := grpcactor.Require(context.Background(), code)
	if !errors.Is(err, grpcactor.ErrNoActor) {
		t.Fatalf("Require error %v does not wrap ErrNoActor", err)
	}
	if got, ok := apperr.Code(err); !ok || got != code {
		t.Fatalf("apperr.Code = %d, %v; want %d, true", got, ok, code)
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestRequirePresent(t *testing.T) {
	want := grpcactor.Actor{Subject: userSubject}
	got, err := grpcactor.Require(grpcactor.WithActor(context.Background(), want), 4011)
	if err != nil || got != want {
		t.Fatalf("Require = %+v, %v; want %+v, nil", got, err, want)
	}
}
