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
	"google.golang.org/protobuf/proto"

	"cloud.google.com/go/ai/generativelanguage/apiv1/generativelanguagepb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config := portals.Config{
		DefaultTargetURL:  "https://generativelanguage.googleapis.com",
		DefaultAuthHeader: "x-goog-api-key",
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
	log.Printf("--- Gemini Portal: Request Intercepted ---")
	log.Printf("URL: %s", req.URL.String())
	log.Printf("Method: %s", req.Method)

	for k, v := range req.Header {
		if strings.ToLower(k) == "x-goog-api-key" || strings.ToLower(k) == "authorization" {
			log.Printf("Header %s: [REDACTED]", k)
			continue
		}
		log.Printf("Header %s: %v", k, v)
	}

	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			logRequestBody(req, bodyBytes)
		}
	}

	resp, err := t.underlying.RoundTrip(req)
	if err != nil {
		log.Printf("Error sending request: %v", err)
		return resp, err
	}

	log.Printf("--- Gemini Portal: Response Intercepted ---")
	log.Printf("Status: %s", resp.Status)

	for k, v := range resp.Header {
		log.Printf("Header %s: %v", k, v)
	}

	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			logResponseBody(resp, bodyBytes)
		}
	}

	return resp, nil
}

func logRequestBody(req *http.Request, bodyBytes []byte) {
	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var payload struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			SystemInstruction struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"system_instruction"`
		}
		if err := json.Unmarshal(bodyBytes, &payload); err == nil && len(payload.Contents) > 0 {
			if len(payload.SystemInstruction.Parts) > 0 {
				log.Printf("Parsed Request System Prompt: %s", payload.SystemInstruction.Parts[0].Text)
			}
			for _, c := range payload.Contents {
				for _, p := range c.Parts {
					log.Printf("Parsed Request Message [%s]: %s", c.Role, p.Text)
				}
			}
		} else {
			log.Printf("Request Body: %s", string(bodyBytes))
		}
	} else if strings.HasPrefix(contentType, "application/grpc") {
		if len(bodyBytes) >= 5 {
			msgBytes := bodyBytes[5:]
			var reqProto generativelanguagepb.GenerateContentRequest
			if err := proto.Unmarshal(msgBytes, &reqProto); err == nil {
				for _, c := range reqProto.GetContents() {
					for _, p := range c.GetParts() {
						if textPart, ok := p.Data.(*generativelanguagepb.Part_Text); ok {
							log.Printf("Parsed GRPC Request Message [%s]: %s", c.GetRole(), textPart.Text)
						}
					}
				}
			} else {
				log.Printf("Failed to unmarshal GRPC GenerateContentRequest: %v", err)
			}
		}
	} else {
		log.Printf("Request Body (%d bytes)", len(bodyBytes))
	}
}

func logResponseBody(resp *http.Response, bodyBytes []byte) {
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var payload struct {
			Candidates []struct {
				Content struct {
					Role  string `json:"role"`
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(bodyBytes, &payload); err == nil && len(payload.Candidates) > 0 {
			for _, c := range payload.Candidates {
				for _, p := range c.Content.Parts {
					log.Printf("Parsed Response Content [%s]: %s", c.Content.Role, p.Text)
				}
			}
		} else {
			log.Printf("Response Body: %s", string(bodyBytes))
		}
	} else if strings.HasPrefix(contentType, "application/grpc") {
		if len(bodyBytes) >= 5 {
			msgBytes := bodyBytes[5:]
			var respProto generativelanguagepb.GenerateContentResponse
			if err := proto.Unmarshal(msgBytes, &respProto); err == nil {
				for _, c := range respProto.GetCandidates() {
					if c.Content != nil {
						for _, p := range c.Content.GetParts() {
							if textPart, ok := p.Data.(*generativelanguagepb.Part_Text); ok {
								log.Printf("Parsed GRPC Response Content [%s]: %s", c.Content.GetRole(), textPart.Text)
							}
						}
					}
				}
			} else {
				log.Printf("Failed to unmarshal GRPC GenerateContentResponse: %v", err)
			}
		}
	} else {
		log.Printf("Response Body (%d bytes)", len(bodyBytes))
	}
}
