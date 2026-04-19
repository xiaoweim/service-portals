// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingTransport(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gw := gzip.NewWriter(w)
		gw.Write([]byte(`{"content":[{"type":"text","text":"hello world"}]}`))
		gw.Close()
	}))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL, nil)
	// Avoid default transport auto-decompressing so we can test our logic
	transport := &http.Transport{DisableCompression: true}
	lt := &loggingTransport{underlying: transport, targetHost: req.URL.Host}

	resp, err := lt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Parsed Response Content [text]: hello world")) {
		t.Errorf("Expected log to contain uncompressed text, got: %s", output)
	}

	// Make sure resp.Body is still gzipped
	bodyBytes, _ := io.ReadAll(resp.Body)
	gr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("Expected response body to be gzipped, got error: %v", err)
	}
	uncompressedBytes, _ := io.ReadAll(gr)
	if string(uncompressedBytes) != `{"content":[{"type":"text","text":"hello world"}]}` {
		t.Errorf("Unexpected uncompressed bytes: %s", uncompressedBytes)
	}
}
