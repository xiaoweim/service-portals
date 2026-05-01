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

package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"cloud.google.com/go/ai/generativelanguage/apiv1/generativelanguagepb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

type LoggingTransport struct {
	Underlying http.RoundTripper
}

func (t *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

	underlying := t.Underlying
	if underlying == nil {
		underlying = http.DefaultTransport
	}

	resp, err := underlying.RoundTrip(req)
	if err != nil {
		log.Printf("Error sending request: %v", err)
		return resp, err
	}

	log.Printf("--- Gemini Portal: Response Intercepted ---")
	log.Printf("Status: %s", resp.Status)

	for k, v := range resp.Header {
		log.Printf("Header %s: %v", k, v)
	}

	// This buffers the entire response, breaking streaming.
	// We'll leave it as is for now if we want the test to fail first,
	// or we can fix it immediately. Wait, the test is to verify it works, so we must fix it.
	// Wait, we can implement a streaming reader that logs data as it goes, or skip logging if it's a stream?
	// The problem is that gRPC uses HTTP/2 streams and `application/grpc`.
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/grpc") {
		// Just log the headers but do not buffer the stream.
		// A full logging of gRPC streams would require intercepting the stream body and decoding it frame by frame,
		// which is complex. For now, let's just bypass body buffering for gRPC streams to fix the bug.
		log.Printf("Bypassing body logging for gRPC streaming response.")
	} else if resp.Body != nil {
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
			Contents          []json.RawMessage `json:"contents"`
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
			unmarshaler := &protojson.UnmarshalOptions{DiscardUnknown: true}
			for _, cBytes := range payload.Contents {
				var c generativelanguagepb.Content
				if err := unmarshaler.Unmarshal(cBytes, &c); err == nil {
					log.Printf("Parsed Request Message [%s]: %s", c.GetRole(), prototext.Format(&c))
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
					log.Printf("Parsed GRPC Request Message [%s]: %s", c.GetRole(), prototext.Format(c))
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
		unmarshaler := &protojson.UnmarshalOptions{DiscardUnknown: true}
		var respProto generativelanguagepb.GenerateContentResponse
		if err := unmarshaler.Unmarshal(bodyBytes, &respProto); err == nil && len(respProto.GetCandidates()) > 0 {
			for _, c := range respProto.GetCandidates() {
				if c.Content != nil {
					log.Printf("Parsed Response Content [%s]: %s", c.Content.GetRole(), prototext.Format(c.Content))
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
						log.Printf("Parsed GRPC Response Content [%s]: %s", c.Content.GetRole(), prototext.Format(c.Content))
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
