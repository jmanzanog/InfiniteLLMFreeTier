package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
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
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
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
		log.Printf("[Provider] Swapping model '%s' to '%s' for provider %s", localReq.Model, p.config.DefaultModel, p.config.Name)
		localReq.Model = p.config.DefaultModel
	}

	// Critical: Remove null fields as some providers (Groq, Mistral) reject them.

	// 1. Initial marshal
	tempBody, err := json.Marshal(localReq)
	if err != nil {
		return nil, err
	}

	// 2. Unmarshal to map
	var rawMap map[string]interface{}
	if err := json.Unmarshal(tempBody, &rawMap); err != nil {
		return nil, err
	}

	// 3. Recursively remove nulls
	var cleanMap func(m map[string]interface{})
	cleanMap = func(m map[string]interface{}) {
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
	cleanMap(rawMap)

	// 4. Final clean marshal
	body, err := json.Marshal(rawMap)
	if err != nil {
		return nil, err
	}

	// Log truncated body for debugging
	logBody := string(body)
	if len(logBody) > 200 {
		logBody = logBody[:200] + "..."
	}
	log.Printf("[Provider] %s Request Body: %s", p.config.Name, logBody)

	return body, nil
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
		client: &http.Client{Timeout: 60 * time.Second},
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
		client: &http.Client{Timeout: 60 * time.Second},
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
		client: &http.Client{Timeout: 60 * time.Second},
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
		client: &http.Client{Timeout: 60 * time.Second},
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
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
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
	// 1. Convert OpenAI request to Gemini Native format
	geminiReq := geminiRequest{}
	for _, msg := range req.Messages {
		role := "user"
		if string(msg.Role) == "assistant" {
			role = "model"
		}
		// Simplify: Assume content is string. Extract via marshal/unmarshal to avoid complex type assertion.
		textVal := ""

		b, _ := json.Marshal(msg)
		var tempMap map[string]interface{}
		_ = json.Unmarshal(b, &tempMap)

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

	bodyBytes, err := json.Marshal(geminiReq)
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
	defer resp.Body.Close()

	// 4. Read Gemini response
	geminiRespBytes, err := io.ReadAll(resp.Body)
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
	if err := json.Unmarshal(geminiRespBytes, &gResp); err != nil {
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

	openaiBytes, err := json.Marshal(openaiResp)
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
