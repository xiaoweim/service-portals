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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gke-labs/service-portals/pkg/portals"
	"github.com/gke-labs/service-portals/pkg/proxy"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config := portals.Config{
		DefaultTargetURL:  "https://api.anthropic.com",
		DefaultAuthHeader: "x-api-key",
		SetupProxy: func(p *proxy.HTTPProxy) {
			p.Transport = &loggingTransport{
				underlying: http.DefaultTransport,
			}
		},
	}

	if err := portals.Run(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

type loggingTransport struct {
	underlying http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	log.Printf("--- Anthropic Portal: Request Intercepted ---")
	log.Printf("URL: %s", req.URL.String())
	log.Printf("Method: %s", req.Method)

	for k, v := range req.Header {
		if k == "X-Api-Key" || k == "Authorization" {
			log.Printf("Header %s: [REDACTED]", k)
			continue
		}
		log.Printf("Header %s: %v", k, v)
	}

	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			var payload struct {
				Messages []struct {
					Role    string      `json:"role"`
					Content interface{} `json:"content"`
				} `json:"messages"`
				System interface{} `json:"system"`
			}
			if err := json.Unmarshal(bodyBytes, &payload); err == nil && len(payload.Messages) > 0 {
				log.Printf("Parsed Request System Prompt: %v", payload.System)
				for _, m := range payload.Messages {
					log.Printf("Parsed Request Message [%s]: %v", m.Role, m.Content)
				}
			} else {
				log.Printf("Request Body: %s", string(bodyBytes))
			}
		}
	}

	resp, err := t.underlying.RoundTrip(req)
	if err != nil {
		log.Printf("Error sending request: %v", err)
		return resp, err
	}

	log.Printf("--- Anthropic Portal: Response Intercepted ---")
	log.Printf("Status: %s", resp.Status)

	for k, v := range resp.Header {
		log.Printf("Header %s: %v", k, v)
	}

	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			uncompressedBytes := bodyBytes
			if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
				gr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
				if err == nil {
					if b, err := io.ReadAll(gr); err == nil {
						uncompressedBytes = b
					}
					gr.Close()
				}
			}

			var payload struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(uncompressedBytes, &payload); err == nil && len(payload.Content) > 0 {
				for _, c := range payload.Content {
					if c.Type == "text" {
						log.Printf("Parsed Response Content [text]: %s", c.Text)
					}
				}
			} else {
				log.Printf("Response Body: %s", string(uncompressedBytes))
			}
		}
	}

	return resp, nil
}
