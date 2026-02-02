package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type dummyServer struct {
	called bool
	resp   CreateChatCompletionResponseObject
	err    error
}

func (d *dummyServer) CreateChatCompletion(_ context.Context, _ CreateChatCompletionRequestObject) (CreateChatCompletionResponseObject, error) {
	d.called = true
	if d.err != nil {
		return nil, d.err
	}
	return d.resp, nil
}

type errorResponse struct{}

func (e errorResponse) VisitCreateChatCompletionResponse(_ http.ResponseWriter) error {
	return errors.New("visit fail")
}

func TestUnimplemented(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))

	Unimplemented{}.CreateChatCompletion(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestServerInterfaceWrapper_Middleware(t *testing.T) {
	called := false
	server := &struct{ ServerInterface }{}
	server.ServerInterface = ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	wrapper := ServerInterfaceWrapper{
		Handler: server,
		HandlerMiddlewares: []MiddlewareFunc{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-MW", "1")
					next.ServeHTTP(w, r)
				})
			},
		},
	}

	r := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	wrapper.CreateChatCompletion(w, r)

	if !called {
		t.Fatal("expected handler to be called")
	}
	if w.Header().Get("X-MW") != "1" {
		t.Fatal("expected middleware header")
	}
}

func TestHandlerWithOptions_Defaults(t *testing.T) {
	server := ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := HandlerWithOptions(server, ChiServerOptions{})

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandlerWithOptions_CustomErrorHandler(t *testing.T) {
	server := ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := HandlerWithOptions(server, ChiServerOptions{
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusTeapot)
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandler(t *testing.T) {
	server := ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := Handler(server)

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandlerFromMux(t *testing.T) {
	server := ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r := chi.NewRouter()
	h := HandlerFromMux(server, r)

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandlerFromMuxWithBaseURL(t *testing.T) {
	server := ServerInterfaceFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r := chi.NewRouter()
	h := HandlerFromMuxWithBaseURL(server, r, "/v1")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestCreateChatCompletion200JSONResponse(t *testing.T) {
	resp := CreateChatCompletion200JSONResponse{Id: "x", Model: "m"}
	w := httptest.NewRecorder()

	if err := resp.VisitCreateChatCompletionResponse(w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", ct)
	}
}

func TestStrictHandler_DecodeError(t *testing.T) {
	sh := NewStrictHandler(&dummyServer{}, nil)
	request := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{"))
	w := httptest.NewRecorder()

	sh.CreateChatCompletion(w, request)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStrictHandler_ResponseError(t *testing.T) {
	sh := NewStrictHandler(&dummyServer{err: errors.New("fail")}, nil)
	request := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
	w := httptest.NewRecorder()

	sh.CreateChatCompletion(w, request)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestStrictHandler_VisitError(t *testing.T) {
	sh := NewStrictHandler(&dummyServer{resp: errorResponse{}}, nil)
	request := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
	w := httptest.NewRecorder()

	sh.CreateChatCompletion(w, request)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestStrictHandler_UnexpectedResponseType(t *testing.T) {
	sh := NewStrictHandler(&dummyServer{resp: CreateChatCompletion200JSONResponse{Id: "x"}}, []StrictMiddlewareFunc{
		func(next StrictHandlerFunc, _ string) StrictHandlerFunc {
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
				_, _ = next(ctx, w, r, request)
				return "bad", nil
			}
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
	w := httptest.NewRecorder()

	sh.CreateChatCompletion(w, request)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestNewStrictHandlerWithOptions(t *testing.T) {
	options := StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusTeapot)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	sh := NewStrictHandlerWithOptions(&dummyServer{err: errors.New("fail")}, nil, options)
	request := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString(`{"model":"m"}`))
	w := httptest.NewRecorder()

	sh.CreateChatCompletion(w, request)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}

	sh2 := NewStrictHandlerWithOptions(&dummyServer{}, nil, options)
	badReq := httptest.NewRequest(http.MethodPost, "/chat/completions", bytes.NewBufferString("{"))
	w2 := httptest.NewRecorder()

	sh2.CreateChatCompletion(w2, badReq)

	if w2.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", w2.Code)
	}
}

func TestErrors(t *testing.T) {
	root := errors.New("root")

	ue := &UnescapedCookieParamError{ParamName: "x", Err: root}
	if ue.Error() == "" {
		t.Fatal("expected error string")
	}
	if !errors.Is(ue, root) {
		t.Fatal("expected unwrap")
	}

	ue2 := &UnmarshalingParamError{ParamName: "x", Err: root}
	if ue2.Error() == "" {
		t.Fatal("expected error string")
	}
	if !errors.Is(ue2, root) {
		t.Fatal("expected unwrap")
	}

	req := &RequiredParamError{ParamName: "x"}
	if req.Error() == "" {
		t.Fatal("expected error string")
	}

	rh := &RequiredHeaderError{ParamName: "x", Err: root}
	if rh.Error() == "" {
		t.Fatal("expected error string")
	}
	if !errors.Is(rh, root) {
		t.Fatal("expected unwrap")
	}

	ifp := &InvalidParamFormatError{ParamName: "x", Err: root}
	if ifp.Error() == "" {
		t.Fatal("expected error string")
	}
	if !errors.Is(ifp, root) {
		t.Fatal("expected unwrap")
	}

	tmv := &TooManyValuesForParamError{ParamName: "x", Count: 2}
	if tmv.Error() == "" {
		t.Fatal("expected error string")
	}
}

func TestDefaultErrorHandlerFunc(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	defaultErrorHandlerFunc(w, r, errors.New("boom"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

type ServerInterfaceFunc func(w http.ResponseWriter, r *http.Request)

func (f ServerInterfaceFunc) CreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	f(w, r)
}
