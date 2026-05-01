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
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cloud.google.com/go/ai/generativelanguage/apiv1/generativelanguagepb"
	"github.com/gke-labs/service-portals/gemini-portal/pkg/transport"
	"github.com/gke-labs/service-portals/pkg/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type mockServer struct {
	generativelanguagepb.UnimplementedGenerativeServiceServer
}

func (s *mockServer) GenerateContent(ctx context.Context, req *generativelanguagepb.GenerateContentRequest) (*generativelanguagepb.GenerateContentResponse, error) {
	return &generativelanguagepb.GenerateContentResponse{
		Candidates: []*generativelanguagepb.Candidate{
			{
				Content: &generativelanguagepb.Content{
					Role: "model",
					Parts: []*generativelanguagepb.Part{
						{Data: &generativelanguagepb.Part_Text{Text: "Hello non-streaming"}},
					},
				},
			},
		},
	}, nil
}

func (s *mockServer) StreamGenerateContent(req *generativelanguagepb.GenerateContentRequest, stream generativelanguagepb.GenerativeService_StreamGenerateContentServer) error {
	responses := []string{"Hello", " ", "streaming"}
	for _, text := range responses {
		err := stream.Send(&generativelanguagepb.GenerateContentResponse{
			Candidates: []*generativelanguagepb.Candidate{
				{
					Content: &generativelanguagepb.Content{
						Role: "model",
						Parts: []*generativelanguagepb.Part{
							{Data: &generativelanguagepb.Part_Text{Text: text}},
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func TestGRPCForwarding(t *testing.T) {
	// Start a mock gRPC server
	grpcServer := grpc.NewServer()
	generativelanguagepb.RegisterGenerativeServiceServer(grpcServer, &mockServer{})

	// Start httptest TLS server serving the gRPC server
	backend := httptest.NewUnstartedServer(grpcServer)
	backend.EnableHTTP2 = true
	backend.StartTLS()
	defer backend.Close()

	targetURL, _ := url.Parse(backend.URL)

	// Create custom transport for the proxy that trusts the backend's cert
	backendTransport := backend.Client().Transport.(*http.Transport).Clone()

	p, err := proxy.NewHTTPProxy(targetURL, "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	p.Transport = &transport.LoggingTransport{Underlying: backendTransport}

	// Start the proxy server
	proxyServer := httptest.NewUnstartedServer(p)
	proxyServer.EnableHTTP2 = true
	proxyServer.StartTLS()
	defer proxyServer.Close()

	// Create a gRPC client pointing to the proxy
	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	conn, err := grpc.Dial(proxyServer.URL[8:], grpc.WithTransportCredentials(creds)) // Strip https://
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := generativelanguagepb.NewGenerativeServiceClient(conn)

	// Test non-streaming call
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.GenerateContent(ctx, &generativelanguagepb.GenerateContentRequest{})
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content.Parts[0].GetText() != "Hello non-streaming" {
		t.Fatalf("unexpected response: %v", resp)
	}

	// Test streaming call
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	stream, err := client.StreamGenerateContent(ctx2, &generativelanguagepb.GenerateContentRequest{})
	if err != nil {
		t.Fatalf("StreamGenerateContent failed: %v", err)
	}

	var streamTexts []string
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("stream.Recv failed: %v", err)
		}
		streamTexts = append(streamTexts, msg.Candidates[0].Content.Parts[0].GetText())
	}

	expected := []string{"Hello", " ", "streaming"}
	if len(streamTexts) != len(expected) {
		t.Fatalf("expected %d messages, got %d", len(expected), len(streamTexts))
	}
	for i, text := range streamTexts {
		if text != expected[i] {
			t.Fatalf("expected msg %d to be %q, got %q", i, expected[i], text)
		}
	}
}
