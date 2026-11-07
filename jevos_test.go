package jevos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAsk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Questions["q"].Type != TypeYesNo {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"model":"jevos-v5","answers":{"q":{"type":"noul","noul":0.97}},"usage":{"input_tokens":28,"output_tokens":0}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithAPIKey("k"))
	p, err := c.Ask(context.Background(), "text", "Is this a billing problem?")
	if err != nil || p != 0.97 {
		t.Fatalf("got %v, %v", p, err)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := New(WithBaseURL(srv.URL)).Ask(context.Background(), "x", "y")
	if e, ok := err.(*APIError); !ok || e.StatusCode != 401 {
		t.Fatalf("expected APIError 401, got %v", err)
	}
}

func TestAskChoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"jevos-v5","answers":{"q":{"type":"choice","choice":"ad","probabilities":{"ad":0.98,"shady_job":0.011,"normal":0.0068},"confidence":0.973}},"usage":{"input_tokens":542,"output_tokens":0}}`))
	}))
	defer srv.Close()

	a, err := New(WithBaseURL(srv.URL)).AskChoice(context.Background(), "x", "type?", map[string]string{"ad": "реклама", "normal": "обычное"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Choice != "ad" || a.Probabilities["ad"] != 0.98 || a.Confidence != 0.973 {
		t.Fatalf("unexpected answer: %+v", a)
	}
}

func TestAskScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"jevos-v5","answers":{"q":{"type":"score","score":1.0665,"legend":{"0":"calm","1":"annoyed","2":"angry","3":"furious"},"probabilities":{"0":0.279,"1":0.446,"2":0.203,"3":0.071},"confidence":0.3749}},"usage":{"input_tokens":145,"output_tokens":0}}`))
	}))
	defer srv.Close()

	a, err := New(WithBaseURL(srv.URL)).AskScore(context.Background(), "x", "anger?", "calm", "annoyed", "angry", "furious")
	if err != nil {
		t.Fatal(err)
	}
	if a.Score != 1.0665 || a.Level() != "annoyed" || a.Probabilities["1"] != 0.446 || a.Confidence != 0.3749 {
		t.Fatalf("unexpected answer: %+v", a)
	}
}

func TestQueryBuilder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Questions) != 3 {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"model":"jevos-v5","answers":{"a":{"type":"noul","noul":0.5},"b":{"type":"choice","choice":"x","probabilities":{"x":1},"confidence":1},"c":{"type":"score","score":2,"legend":{"0":"l","1":"m","2":"h"},"probabilities":{"2":1},"confidence":1}},"usage":{}}`))
	}))
	defer srv.Close()

	res, err := New(WithBaseURL(srv.URL)).Query("text").
		YesNo("a", "q?").
		Choice("b", "q?", map[string]string{"x": "X"}).
		Score("c", "q?", "l", "m", "h").
		Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c, _ := res.Get("c")
	if c.Level() != "h" {
		t.Fatalf("got %q", c.Level())
	}

	if _, err := New().Query("t").YesNo("a", "q").YesNo("a", "q").Do(context.Background()); err == nil {
		t.Fatal("expected duplicate name error")
	}
}
