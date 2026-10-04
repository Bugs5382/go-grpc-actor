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
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// MetadataKey is the gRPC metadata key the actor travels under. It is a binary
// key, so gRPC base64-encodes the value on the wire; the value is one JSON
// object: {"v":1,"sub":...,"imp":...,"ten":...,"sid":...}, with empty fields
// left out. One key keeps the actor atomic: a caller cannot mix a trusted
// subject with an impersonator from somewhere else.
const MetadataKey = "x-grpc-actor-bin"

// MaxWireLen is the largest encoded actor a server will decode.
const MaxWireLen = 4096

const wireVersion = 1

type wireActor struct {
	V   int    `json:"v"`
	Sub string `json:"sub"`
	Imp string `json:"imp,omitempty"`
	Ten string `json:"ten,omitempty"`
	Sid string `json:"sid,omitempty"`
}

func encode(a Actor) (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(wireActor{V: wireVersion, Sub: a.Subject, Imp: a.Impersonator, Ten: a.Tenant, Sid: a.Session}); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidActor, err)
	}
	return string(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

func decode(vals []string) (Actor, error) {
	if len(vals) != 1 {
		return Actor{}, fmt.Errorf("%w: %d values for %s, want 1", ErrInvalidActor, len(vals), MetadataKey)
	}
	raw := vals[0]
	if len(raw) > MaxWireLen {
		return Actor{}, fmt.Errorf("%w: encoded actor is longer than %d bytes", ErrInvalidActor, MaxWireLen)
	}
	if !utf8.ValidString(raw) {
		return Actor{}, fmt.Errorf("%w: encoded actor is not valid UTF-8", ErrInvalidActor)
	}
	var w wireActor
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return Actor{}, fmt.Errorf("%w: %v", ErrInvalidActor, err)
	}
	if w.V != wireVersion {
		return Actor{}, fmt.Errorf("%w: wire version %d, want %d", ErrInvalidActor, w.V, wireVersion)
	}
	a := Actor{Subject: w.Sub, Impersonator: w.Imp, Tenant: w.Ten, Session: w.Sid}
	if err := a.Validate(); err != nil {
		return Actor{}, err
	}
	return a, nil
}
