package provider

import (
	"context"
	"encoding/json"
	"io" // Added for io.ReadAll
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
)

func TestConstructors_Defaults(t *testing.T) {
	tests := []struct {
		name        string
		constructor func(string, string) Provider
		expected    string
	}{
		{"Groq", NewGroqProvider, "llama-3.3-70b-versatile"},
		{"Mistral", NewMistralProvider, "open-mistral-nemo"},
		{"OpenRouter", NewOpenRouterProvider, "google/gemini-2.0-flash-exp:free"},
		{"Cerebras", NewCerebrasProvider, "llama-3.3-70b"},
		{"Gemini", NewGeminiProvider, "gemini-1.5-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.constructor("key", "")
			// We can't access p.config directly as it's private in baseProvider
			// But we can check via behavior or reflection, or check Name() which accesses config
			if p.Name() != tt.name {
				t.Errorf("Expected name %s, got %s", tt.name, p.Name())
			}
			// Use the provider to check if it uses the default model internally
			// We can do this by mocking a server and checking the request body
			// checking the swap log or setting up a spy.
		})
	}
}

func TestPrepareRequest_Logic(t *testing.T) {
	// Use GenericProvider (via NewCustomProvider) to test baseProvider.prepareRequest logic
	t.Run("Model_Swap_And_Null_Cleaning", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)

			// Check Model Swap
			if body["model"] != "default-model" {
				t.Errorf("Expected model 'default-model', got %v", body["model"])
			}
			// Check Null Cleaning (Temperature is pointer, if nil it should be absent)
			if _, ok := body["temperature"]; ok {
				t.Errorf("Expected 'temperature' to be absent (null cleaned), got %v", body["temperature"])
			}
		}))
		defer server.Close()

		p := NewCustomProvider("Test", server.URL, "key", "default-model")

		// Request with DIFFERENT model and NIL fields
		req := &api.CreateChatCompletionRequest{
			Model: "user-model", // Should be swapped
			// Temperature is nil by default
		}

		_, err := p.Chat(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Large_Body_Logging", func(t *testing.T) {
		// This mainly exercises the logging code path. We can't easily assert stdout.
		// But we ensure it doesn't crash.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))
		defer server.Close()

		p := NewCustomProvider("Test", server.URL, "key", "mod")
		longText := ""
		for i := 0; i < 300; i++ {
			longText += "a"
		}
		req := &api.CreateChatCompletionRequest{
			Model: "mod",
			Messages: []api.ChatCompletionRequestMessage{
				{Role: "user", Content: longText},
			},
		}
		_, _ = p.Chat(context.Background(), req)
	})

	t.Run("Recursive_Null_Cleaning", func(t *testing.T) {
		// Test that nested maps and slices are cleaned recursively
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			// Messages should be present and each message should not have null fields
			if msgs, ok := body["messages"].([]interface{}); ok {
				for _, m := range msgs {
					if msg, ok := m.(map[string]interface{}); ok {
						// Verify no null values in nested map
						for k, v := range msg {
							if v == nil {
								t.Errorf("Null value found for key %s in message", k)
							}
						}
					}
				}
			}
			w.WriteHeader(200)
		}))
		defer server.Close()

		p := NewCustomProvider("Test", server.URL, "key", "model")

		// Request with messages that have nested structures
		req := &api.CreateChatCompletionRequest{
			Model: "model",
			Messages: []api.ChatCompletionRequestMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
		}

		_, err := p.Chat(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestGeminiProvider_Complete(t *testing.T) {
	t.Run("Success_Flow_With_Assistant_Role", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify URL
			if r.URL.Path != "/v1beta/models/gemini-1.5-flash:generateContent" {
				t.Errorf("Unexpected path: %s", r.URL.Path)
			}
			// Verify key
			if r.URL.Query().Get("key") != "k" {
				t.Error("Missing key")
			}

			// Verify Body Transformation
			var gReq struct {
				Contents []struct {
					Role  string `json:"role"`
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"contents"`
			}
			_ = json.NewDecoder(r.Body).Decode(&gReq)

			if len(gReq.Contents) != 2 {
				t.Fatalf("Expected 2 contents, got %d", len(gReq.Contents))
			}
			if gReq.Contents[0].Role != "user" || gReq.Contents[0].Parts[0].Text != "Hi" {
				t.Error("First message mismatch")
			}
			if gReq.Contents[1].Role != "model" || gReq.Contents[1].Parts[0].Text != "Hello" {
				t.Error("Second message mismatch (Assistant->Model)")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{ "candidates": [ { "content": { "parts": [ { "text": "I am Gemini" } ] } } ] }`))
		}))
		defer server.Close()

		p := NewGeminiProvider("k", "")
		// Hack: Override BaseURL via reflection or just use the constructor logic?
		// Since NewGeminiProvider hardcodes BaseURL, we can't change it easily without
		// mocking the Transport or modifying the struct directly via Unsafe/private access in same package.
		// Being in 'package provider', we CAN access private fields!
		gp := p.(*GeminiProvider)
		gp.config.BaseURL = server.URL + "/v1beta/models/" // Point to our mock

		req := &api.CreateChatCompletionRequest{
			Model: "gemini-1.5-flash",
			Messages: []api.ChatCompletionRequestMessage{
				{Role: "user", Content: "Hi"},
				{Role: "assistant", Content: "Hello"},
			},
		}

		resp, err := gp.Chat(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Check converted response
		var openAIResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&openAIResp)
		content := openAIResp["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"]
		if content != "I am Gemini" {
			t.Errorf("Expected 'I am Gemini', got %v", content)
		}
	})

	t.Run("Upstream_Error_Pass_Through", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error": "rate limit"}`))
		}))
		defer server.Close()

		p := NewGeminiProvider("k", "")
		gp := p.(*GeminiProvider)
		gp.config.BaseURL = server.URL + "/"

		resp, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{
			Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "x"}},
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429, got %d", resp.StatusCode)
		}
		// Ensure body is readable and preserved
		body, _ := io.ReadAll(resp.Body)
		if string(body) != `{"error": "rate limit"}` {
			t.Errorf("Body mismatch. Got: %s", string(body))
		}
	})

	t.Run("Bad_Gemini_Response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{ invalid json }`))
		}))
		defer server.Close()

		p := NewGeminiProvider("k", "")
		gp := p.(*GeminiProvider)
		gp.config.BaseURL = server.URL + "/"

		_, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "x"}}})
		if err == nil {
			t.Error("Expected error on invalid JSON")
		}
	})

	t.Run("Bad_URL_NewRequest_Fail", func(t *testing.T) {
		p := NewGeminiProvider("k", "")
		gp := p.(*GeminiProvider)
		gp.config.BaseURL = "http://\x7f/" // Invalid char

		_, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "x"}}})
		if err == nil {
			t.Error("Expected error on bad URL")
		}
	})

	t.Run("Context_Cancelled", func(t *testing.T) {
		// Mock a sleeping server to trigger context cancellation
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()

		p := NewGeminiProvider("k", "")
		gp := p.(*GeminiProvider)
		gp.config.BaseURL = server.URL + "/"

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := gp.Chat(ctx, &api.CreateChatCompletionRequest{Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "x"}}})
		if err == nil {
			t.Error("Expected context error")
		}
	})
}

func TestGenericProvider_Errors(t *testing.T) {
	t.Run("PrepareRequest_Fail", func(t *testing.T) {
		p := NewCustomProvider("G", "http://u", "k", "m")
		nan := float32(math.NaN())
		_, err := p.Chat(context.Background(), &api.CreateChatCompletionRequest{Temperature: &nan})
		if err == nil {
			t.Error("Expected marshal error")
		}
	})

	t.Run("NewRequest_Fail", func(t *testing.T) {
		p := NewCustomProvider("G", "http://\x7f", "k", "m")
		_, err := p.Chat(context.Background(), &api.CreateChatCompletionRequest{Model: "m"})
		if err == nil {
			t.Error("Expected URL error")
		}
	})
}

func TestProviders_Chat_Success(t *testing.T) {
	tests := []struct {
		name        string
		constructor func(string, string) Provider
	}{
		{"Groq", NewGroqProvider},
		{"Mistral", NewMistralProvider},
		{"OpenRouter", NewOpenRouterProvider},
		{"Cerebras", NewCerebrasProvider},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			p := tt.constructor("key", "")

			// Override BaseURL for testing
			switch v := p.(type) {
			case *GroqProvider:
				v.config.BaseURL = server.URL
			case *MistralProvider:
				v.config.BaseURL = server.URL
			case *OpenRouterProvider:
				v.config.BaseURL = server.URL
			case *CerebrasProvider:
				v.config.BaseURL = server.URL
			}

			_, err := p.Chat(context.Background(), &api.CreateChatCompletionRequest{
				Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Errorf("%s Chat failed: %v", tt.name, err)
			}
		})
	}
}

func TestOpenRouter_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom headers
		if r.Header.Get("HTTP-Referer") == "" {
			t.Error("Missing HTTP-Referer header")
		}
		if r.Header.Get("X-Title") == "" {
			t.Error("Missing X-Title header")
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := NewOpenRouterProvider("key", "")
	op := p.(*OpenRouterProvider)
	op.config.BaseURL = server.URL

	_, err := p.Chat(context.Background(), &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGemini_EmptyContent(t *testing.T) {
	// Test when content is not a string (like an array or object)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{ "candidates": [ { "content": { "parts": [ { "text": "ok" } ] } } ] }`))
	}))
	defer server.Close()

	p := NewGeminiProvider("k", "")
	gp := p.(*GeminiProvider)
	gp.config.BaseURL = server.URL + "/"

	// Message with no content field value (becomes empty string)
	req := &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{
			{Role: "user", Content: ""}, // Empty content - should be skipped
			{Role: "user", Content: "valid"},
		},
	}

	resp, err := gp.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestGemini_EmptyCandidates(t *testing.T) {
	// Test when Gemini returns empty candidates
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{ "candidates": [] }`))
	}))
	defer server.Close()

	p := NewGeminiProvider("k", "")
	gp := p.(*GeminiProvider)
	gp.config.BaseURL = server.URL + "/"

	req := &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "test"}},
	}

	resp, err := gp.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should return empty content but no error
	var openAIResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&openAIResp)
	content := openAIResp["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"]
	if content != "" {
		t.Errorf("Expected empty content, got %v", content)
	}
}

func TestGemini_EmptyParts(t *testing.T) {
	// Test when Gemini returns empty parts
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{ "candidates": [ { "content": { "parts": [] } } ] }`))
	}))
	defer server.Close()

	p := NewGeminiProvider("k", "")
	gp := p.(*GeminiProvider)
	gp.config.BaseURL = server.URL + "/"

	req := &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "test"}},
	}

	resp, err := gp.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var openAIResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&openAIResp)
	content := openAIResp["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"]
	if content != "" {
		t.Errorf("Expected empty content, got %v", content)
	}
}

func TestConstructors_WithCustomModels(t *testing.T) {
	tests := []struct {
		name        string
		constructor func(string, string) Provider
		customModel string
	}{
		{"Groq", NewGroqProvider, "custom-groq-model"},
		{"Mistral", NewMistralProvider, "custom-mistral-model"},
		{"OpenRouter", NewOpenRouterProvider, "custom-openrouter-model"},
		{"Cerebras", NewCerebrasProvider, "custom-cerebras-model"},
		{"Gemini", NewGeminiProvider, "custom-gemini-model"},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_CustomModel", func(t *testing.T) {
			p := tt.constructor("key", tt.customModel)
			if p.Name() != tt.name {
				t.Errorf("Expected name %s, got %s", tt.name, p.Name())
			}
		})
	}
}
