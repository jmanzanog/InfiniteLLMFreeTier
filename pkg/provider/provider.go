package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/httpclient"
)

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

type Provider interface {
	Name() string
	Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error)
}

// Config for providers
type Config struct {
	Name         string
	BaseURL      string
	APIKey       string
	DefaultModel string
}

type baseProvider struct {
	config Config
	client *http.Client
}

func NewCustomProvider(name, baseURL, apiKey, defaultModel string) Provider {
	return &GenericProvider{baseProvider{
		config: Config{
			Name:         name,
			BaseURL:      baseURL,
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

type GenericProvider struct{ baseProvider }

func (p *GenericProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	return p.performChat(ctx, req, nil)
}

func (p *baseProvider) Name() string {
	return p.config.Name
}

// prepareRequest sanitizes and prepares the request payload
func (p *baseProvider) prepareRequest(req *api.CreateChatCompletionRequest) ([]byte, error) {
	// Clone request to avoid mutating original
	localReq := *req

	// Force default model if requested model differs (basic enforcement)
	if localReq.Model != p.config.DefaultModel {
		slog.Info("Swapping model", "component", "provider", "provider_name", p.config.Name, "from", localReq.Model, "to", p.config.DefaultModel)
		localReq.Model = p.config.DefaultModel
	}

	// Critical: Remove null fields as some providers (Groq, Mistral) reject them.

	// 1. Initial marshal
	tempBody, err := jsonMarshal(localReq)
	if err != nil {
		return nil, err
	}

	// 2. Unmarshal to map
	var rawMap map[string]interface{}
	if err := jsonUnmarshal(tempBody, &rawMap); err != nil {
		return nil, err
	}

	// 3. Recursively remove nulls
	cleanMap(rawMap)

	// 4. Final clean marshal
	body, err := jsonMarshal(rawMap)
	if err != nil {
		return nil, err
	}

	// Log truncated body for debugging
	logBody := string(body)
	if len(logBody) > 200 {
		logBody = logBody[:200] + "..."
	}
	slog.Info("Request Body", "component", "provider", "provider_name", p.config.Name, "body_truncated", logBody)

	return body, nil
}

func cleanMap(m map[string]interface{}) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
		} else if nestedMap, ok := v.(map[string]interface{}); ok {
			cleanMap(nestedMap)
		} else if nestedSlice, ok := v.([]interface{}); ok {
			for _, item := range nestedSlice {
				if itemMap, ok := item.(map[string]interface{}); ok {
					cleanMap(itemMap)
				}
			}
		}
	}
}

// performChat handles common HTTP logic: prepare, auth, send.
func (p *baseProvider) performChat(ctx context.Context, req *api.CreateChatCompletionRequest, extraHeaders map[string]string) (*http.Response, error) {
	body, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		httpReq.Header.Set(k, v)
	}
	return p.client.Do(httpReq)
}

// --- GROQ ---

type GroqProvider struct{ baseProvider }

func NewGroqProvider(apiKey, defaultModel string) Provider {
	if defaultModel == "" {
		defaultModel = "llama-3.3-70b-versatile"
	}
	return &GroqProvider{baseProvider{
		config: Config{
			Name:         "Groq",
			BaseURL:      "https://api.groq.com/openai/v1/chat/completions",
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

func (p *GroqProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	return p.performChat(ctx, req, nil)
}

// --- MISTRAL ---

type MistralProvider struct{ baseProvider }

func NewMistralProvider(apiKey, defaultModel string) Provider {
	if defaultModel == "" {
		defaultModel = "open-mistral-nemo"
	}
	return &MistralProvider{baseProvider{
		config: Config{
			Name:         "Mistral",
			BaseURL:      "https://api.mistral.ai/v1/chat/completions",
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

func (p *MistralProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	return p.performChat(ctx, req, nil)
}

// --- OPENROUTER ---

type OpenRouterProvider struct{ baseProvider }

func NewOpenRouterProvider(apiKey, defaultModel string) Provider {
	if defaultModel == "" {
		defaultModel = "google/gemini-2.0-flash-exp:free"
	}
	return &OpenRouterProvider{baseProvider{
		config: Config{
			Name:         "OpenRouter",
			BaseURL:      "https://openrouter.ai/api/v1/chat/completions",
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

func (p *OpenRouterProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	headers := map[string]string{
		"HTTP-Referer": "https://github.com/jmanzanog/InfiniteLLM",
		"X-Title":      "InfiniteLLM Gateway",
	}
	return p.performChat(ctx, req, headers)
}

// --- CEREBRAS ---

type CerebrasProvider struct{ baseProvider }

func NewCerebrasProvider(apiKey, defaultModel string) Provider {
	if defaultModel == "" {
		defaultModel = "llama-3.3-70b"
	}
	return &CerebrasProvider{baseProvider{
		config: Config{
			Name:         "Cerebras",
			BaseURL:      "https://api.cerebras.ai/v1/chat/completions",
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

func (p *CerebrasProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	return p.performChat(ctx, req, nil)
}

// --- GEMINI (Native API Adapter) ---

type GeminiProvider struct{ baseProvider }

func NewGeminiProvider(apiKey, defaultModel string) Provider {
	if defaultModel == "" {
		defaultModel = "gemini-1.5-flash"
	}
	// Native API Base URL
	return &GeminiProvider{baseProvider{
		config: Config{
			Name:         "Gemini",
			BaseURL:      "https://generativelanguage.googleapis.com/v1beta/models/",
			APIKey:       apiKey,
			DefaultModel: defaultModel,
		},
		client: httpclient.New(),
	}}
}

// Gemini Native API Structures
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}
type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (p *GeminiProvider) Chat(ctx context.Context, req *api.CreateChatCompletionRequest) (*http.Response, error) {
	// Check for streaming request - Gemini native API requires SSE for streaming
	// which is not yet implemented. Return a clear error instead of silent failure.
	if req.Stream != nil && *req.Stream {
		slog.Warn("Streaming requested but not supported for Gemini provider",
			"provider", p.config.Name,
			"model", p.config.DefaultModel,
		)
		// Return a 501 Not Implemented response
		errorResp := map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Streaming (stream=true) is not supported for Gemini provider. Use stream=false or omit the parameter.",
				"type":    "not_implemented",
				"code":    "streaming_not_supported",
			},
		}
		errorBytes, _ := jsonMarshal(errorResp)
		return &http.Response{
			StatusCode:    http.StatusNotImplemented,
			Body:          io.NopCloser(bytes.NewBuffer(errorBytes)),
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			ContentLength: int64(len(errorBytes)),
		}, nil
	}

	// 1. Convert OpenAI request to Gemini Native format
	geminiReq := geminiRequest{}
	for _, msg := range req.Messages {
		role := "user"
		if string(msg.Role) == "assistant" {
			role = "model"
		}
		// Simplify: Assume content is string. Extract via marshal/unmarshal to avoid complex type assertion.
		textVal := ""

		b, _ := jsonMarshal(msg)
		var tempMap map[string]interface{}
		_ = jsonUnmarshal(b, &tempMap)

		if contentStr, ok := tempMap["content"].(string); ok {
			textVal = contentStr
		}

		if textVal != "" {
			geminiReq.Contents = append(geminiReq.Contents, geminiContent{
				Role:  role,
				Parts: []geminiPart{{Text: textVal}},
			})
		}
	}

	bodyBytes, err := jsonMarshal(geminiReq)
	if err != nil {
		return nil, err
	}

	// 2. Build URL
	url := p.config.BaseURL + p.config.DefaultModel + ":generateContent?key=" + p.config.APIKey

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// 3. Execute Request
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	// NOTE: Do NOT defer resp.Body.Close() here. The caller (balancer) is responsible
	// for closing the body. Closing it here would return a closed body, breaking
	// failover logic and response logging.

	// 4. Read Gemini response
	geminiRespBytes, err := io.ReadAll(resp.Body)
	// Close original body after reading since we will return a new one
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// Return non-200 responses as-is for balancer logging
	if resp.StatusCode != http.StatusOK {
		// Restore body for upstream reading
		resp.Body = io.NopCloser(bytes.NewBuffer(geminiRespBytes))
		return resp, nil
	}

	// 5. Parse Gemini response to OpenAI format
	var gResp geminiResponse
	if err := jsonUnmarshal(geminiRespBytes, &gResp); err != nil {
		return nil, err
	}

	content := ""
	if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
		content = gResp.Candidates[0].Content.Parts[0].Text
	}

	// Create OpenAI response structure
	openaiResp := map[string]interface{}{
		"id":      "chatcmpl-gemini-adapter",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   p.config.DefaultModel,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}

	openaiBytes, err := jsonMarshal(openaiResp)
	if err != nil {
		return nil, err
	}

	// 6. Return adapted response
	newResp := &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(bytes.NewBuffer(openaiBytes)),
		Header:        make(http.Header),
		ContentLength: int64(len(openaiBytes)),
	}
	newResp.Header.Set("Content-Type", "application/json")

	return newResp, nil
}
