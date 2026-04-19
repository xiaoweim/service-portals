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
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingTransport(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	transport := &http.Transport{}
	lt := &loggingTransport{underlying: transport}

	resp, err := lt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Header Authorization: [REDACTED]")) {
		t.Errorf("Expected log to redact Authorization header, got: %s", output)
	}
	if bytes.Contains([]byte(output), []byte("secret")) {
		t.Errorf("Expected log to not contain secret, got: %s", output)
	}
}
