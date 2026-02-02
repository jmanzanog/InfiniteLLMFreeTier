package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jmanzanog/InfiniteLLMFreeTier/pkg/api"
)

type errorReadCloser struct{}

func (e *errorReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("read fail")
}

func (e *errorReadCloser) Close() error { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCleanMap_RemovesNestedMapNil(t *testing.T) {
	input := map[string]interface{}{
		"outer": map[string]interface{}{
			"keep": "x",
			"drop": nil,
		},
	}

	cleanMap(input)

	outer := input["outer"].(map[string]interface{})
	if _, ok := outer["drop"]; ok {
		t.Fatal("expected nested nil key to be removed")
	}
}

func TestPrepareRequest_UnmarshalError(t *testing.T) {
	origMarshal := jsonMarshal
	origUnmarshal := jsonUnmarshal
	jsonMarshal = json.Marshal
	jsonUnmarshal = func(_ []byte, _ interface{}) error { return errors.New("unmarshal fail") }
	t.Cleanup(func() {
		jsonMarshal = origMarshal
		jsonUnmarshal = origUnmarshal
	})

	p := NewCustomProvider("Test", "http://x", "k", "m").(*GenericProvider)
	_, err := p.prepareRequest(&api.CreateChatCompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestPrepareRequest_FinalMarshalError(t *testing.T) {
	origMarshal := jsonMarshal
	origUnmarshal := jsonUnmarshal
	callCount := 0
	jsonMarshal = func(v interface{}) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, errors.New("marshal fail")
		}
		return json.Marshal(v)
	}
	jsonUnmarshal = json.Unmarshal
	t.Cleanup(func() {
		jsonMarshal = origMarshal
		jsonUnmarshal = origUnmarshal
	})

	p := NewCustomProvider("Test", "http://x", "k", "m").(*GenericProvider)
	_, err := p.prepareRequest(&api.CreateChatCompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestGemini_MarshalRequestError(t *testing.T) {
	origMarshal := jsonMarshal
	origUnmarshal := jsonUnmarshal
	jsonMarshal = func(v interface{}) ([]byte, error) {
		switch v.(type) {
		case geminiRequest:
			return nil, errors.New("marshal fail")
		default:
			return json.Marshal(v)
		}
	}
	jsonUnmarshal = json.Unmarshal
	t.Cleanup(func() {
		jsonMarshal = origMarshal
		jsonUnmarshal = origUnmarshal
	})

	p := NewGeminiProvider("k", "model")
	gp := p.(*GeminiProvider)
	gp.config.BaseURL = "http://example.invalid/"

	_, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestGemini_ReadResponseError(t *testing.T) {
	p := NewGeminiProvider("k", "model")
	gp := p.(*GeminiProvider)
	gp.client = &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{},
			Header:     make(http.Header),
		}, nil
	})}

	_, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestGemini_MarshalResponseError(t *testing.T) {
	origMarshal := jsonMarshal
	origUnmarshal := jsonUnmarshal
	jsonMarshal = func(v interface{}) ([]byte, error) {
		switch v.(type) {
		case map[string]interface{}:
			return nil, errors.New("marshal fail")
		default:
			return json.Marshal(v)
		}
	}
	jsonUnmarshal = json.Unmarshal
	t.Cleanup(func() {
		jsonMarshal = origMarshal
		jsonUnmarshal = origUnmarshal
	})

	p := NewGeminiProvider("k", "model")
	gp := p.(*GeminiProvider)
	gp.client = &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		body := []byte(`{ "candidates": [ { "content": { "parts": [ { "text": "ok" } ] } } ] }`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBuffer(body)),
			Header:     make(http.Header),
		}, nil
	})}

	_, err := gp.Chat(context.Background(), &api.CreateChatCompletionRequest{
		Messages: []api.ChatCompletionRequestMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}
