package openaiutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailoverRetriesOn5xx(t *testing.T) {
	var primaryHits, fallbackHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		if auth := r.Header.Get("Authorization"); auth != "Bearer key1" {
			t.Errorf("primary auth = %q", auth)
		}
		http.Error(w, "upstream broken", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		if auth := r.Header.Get("Authorization"); auth != "Bearer key2" {
			t.Errorf("fallback auth = %q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"deepseek-v4-flash"`) {
			t.Errorf("fallback body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer fallback.Close()

	tr := &failoverTransport{
		primary:  Endpoint{BaseURL: primary.URL, APIKey: "key1", Model: "gpt-5-6-instant"},
		fallback: &Endpoint{BaseURL: fallback.URL, APIKey: "key2", Model: "deepseek-v4-flash"},
		transport: http.DefaultTransport,
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest(http.MethodPost, primary.URL+"/chat/completions", strings.NewReader(`{"model":"gpt-5-6-instant","messages":[]}`))
	req.Header.Set("Authorization", "Bearer key1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if primaryHits != 1 || fallbackHits != 1 {
		t.Fatalf("hits primary=%d fallback=%d", primaryHits, fallbackHits)
	}
}

func TestFailoverNotTriggeredOnSuccess(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("fallback must not be hit")
	}))
	defer fallback.Close()

	tr := &failoverTransport{
		primary:  Endpoint{BaseURL: primary.URL, APIKey: "key1", Model: "gpt-5-6-instant"},
		fallback: &Endpoint{BaseURL: fallback.URL, APIKey: "key2", Model: "deepseek-v4-flash"},
		transport: http.DefaultTransport,
	}
	resp, err := (&http.Client{Transport: tr}).Get(primary.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestFailoverOnTransportError(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("connection reset")
	}))
	primaryURL := primary.URL
	primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer fallback.Close()

	tr := &failoverTransport{
		primary:   Endpoint{BaseURL: primaryURL, APIKey: "key1", Model: "gpt-5-6-instant"},
		fallback:  &Endpoint{BaseURL: fallback.URL, APIKey: "key2", Model: "deepseek-v4-flash"},
		transport: http.DefaultTransport,
	}
	resp, err := (&http.Client{Transport: tr}).Get(primaryURL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
