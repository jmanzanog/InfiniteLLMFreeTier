package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/balancer"
)

// Server implements api.StrictServerInterface
type Server struct {
	api.Unimplemented
	lb *balancer.Balancer
}

func NewServer(lb *balancer.Balancer) *Server {
	return &Server{lb: lb}
}

// CreateChatCompletion implements api.StrictServerInterface
func (s *Server) CreateChatCompletion(ctx context.Context, request api.CreateChatCompletionRequestObject) (api.CreateChatCompletionResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("request body is required")
	}

	slog.Info("Incoming request", "model", request.Body.Model)

	result, err := s.lb.ChatWithResult(ctx, request.Body)
	if err != nil {
		slog.Error("Error forwarding request", "error", err)
		return nil, err
	}

	resp := result.Response

	if os.Getenv("LOG_LLM_RESPONSE_DETAILS") == "true" {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Error reading response body for logging", "error", err)
		}
		// Restore the body so it can be read again
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		slog.Info("Upstream response details",
			"status", resp.Status,
			"status_code", resp.StatusCode,
			"headers", resp.Header,
			"body", string(bodyBytes),
		)
	} else {
		slog.Info("Upstream response received", "status", resp.Status, "provider", result.ProviderName, "response_time_ms", result.ResponseTime.Milliseconds())
	}

	// We return a custom response object to handle both JSON and Streaming
	return &ProxyResponse{
		resp:         resp,
		providerName: result.ProviderName,
		responseTime: result.ResponseTime.Milliseconds(),
	}, nil
}

// ProxyResponse implements api.CreateChatCompletionResponseObject
type ProxyResponse struct {
	resp         *http.Response
	providerName string
	responseTime int64
}

func (r *ProxyResponse) VisitCreateChatCompletionResponse(w http.ResponseWriter) error {
	defer func() { _ = r.resp.Body.Close() }()

	// Copy headers from upstream
	for k, vv := range r.resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// Add custom headers with provider info
	w.Header().Set("X-Provider", r.providerName)
	w.Header().Set("X-Response-Time-Ms", fmt.Sprintf("%d", r.responseTime))

	w.WriteHeader(r.resp.StatusCode)

	// Stream or copy body
	if flusher, ok := w.(http.Flusher); ok {
		_, err := io.Copy(&flushWriter{w, flusher}, r.resp.Body)
		return err
	}

	_, err := io.Copy(w, r.resp.Body)
	return err
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	if n > 0 {
		fw.f.Flush()
	}
	return
}
