// Package jevos is a small Go client for the jevos local decision server
// (https://github.com/feder-cr/jev), started with `jev serve`.
//
// It speaks the POST /v1/systemone wire format: you send a text ("state")
// and named questions (yes/no, choice, score) and get probabilities back.
package jevos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "http://127.0.0.1:8017"
	DefaultModel   = "jev-latest"

	TypeYesNo  = "noul"
	TypeChoice = "choice"
	TypeScore  = "score"
)

// Question is a single question asked about the state.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	// Criteria is map[string]string for choice, []string for score.
	Criteria any `json:"criteria,omitempty"`
}

// YesNo builds a yes/no question; the answer is P(yes).
func YesNo(instructions string) Question {
	return Question{Type: TypeYesNo, Instructions: instructions}
}

// Choice builds a multiple-choice question. options maps option name to
// a short description of it.
func Choice(instructions string, options map[string]string) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: options}
}

// Score builds a score question; levels are ordered from lowest to highest.
func Score(instructions string, levels ...string) Question {
	return Question{Type: TypeScore, Instructions: instructions, Criteria: levels}
}

// Request is the body of POST /v1/systemone.
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"` // string or JSON-serializable object
	Questions map[string]Question `json:"questions"`
}

// Answer is the result for one question.
//
// For yes/no questions Noul holds P(yes). For choice questions Choice is the
// most probable option and Probabilities has a probability per option.
// For score questions Score is the expected level (0-based index into the
// criteria list), Legend maps level index to its label, and Probabilities
// is keyed by level index ("0", "1", ...).
// Confidence is reported for choice and score questions: when it is low,
// send the case to a person.
type Answer struct {
	Type          string
	Noul          float64
	Choice        string
	Score         float64
	Legend        map[string]string
	Probabilities map[string]float64
	Confidence    float64
	Raw           map[string]json.RawMessage
}

// Level returns the label of the score level nearest to Score
// (e.g. 1.07 -> "annoyed"). It returns "" if there is no legend.
func (a *Answer) Level() string {
	if len(a.Legend) == 0 {
		return ""
	}
	i := int(math.Round(a.Score))
	if i < 0 {
		i = 0
	}
	if i >= len(a.Legend) {
		i = len(a.Legend) - 1
	}
	return a.Legend[strconv.Itoa(i)]
}

func (a *Answer) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Raw = raw
	get := func(key string, dst any) {
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, dst)
		}
	}
	get("type", &a.Type)
	get("noul", &a.Noul)
	get("choice", &a.Choice)
	get("score", &a.Score)
	get("legend", &a.Legend)
	get("probabilities", &a.Probabilities)
	get("confidence", &a.Confidence)
	return nil
}

// Usage is token accounting reported by the server.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the body returned by /v1/systemone.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jevos: server returned %d: %s", e.StatusCode, e.Body)
}

// Client talks to a jevos server. The zero value is not usable; use New.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL sets the server address, e.g. "http://127.0.0.1:8017".
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithAPIKey sets the bearer token (the server's JEV_API_KEY).
func WithAPIKey(k string) Option { return func(c *Client) { c.apiKey = k } }

// WithModel overrides the model name sent in requests.
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New creates a Client with defaults pointing at a local server.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		model:   DefaultModel,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Decide sends several questions about one state in a single request;
// the server reads the text once.
func (c *Client) Decide(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	if len(questions) == 0 {
		return nil, errors.New("jevos: no questions")
	}
	body, err := json.Marshal(Request{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("jevos: decode response: %w", err)
	}
	return &out, nil
}

// Ask asks one yes/no question and returns P(yes).
func (c *Client) Ask(ctx context.Context, state any, question string) (float64, error) {
	const key = "q"
	r, err := c.Decide(ctx, state, map[string]Question{key: YesNo(question)})
	if err != nil {
		return 0, err
	}
	a, ok := r.Answers[key]
	if !ok {
		return 0, errors.New("jevos: answer missing in response")
	}
	return a.Noul, nil
}

// AskChoice asks one multiple-choice question and returns the full answer
// (Choice, Probabilities, Confidence).
func (c *Client) AskChoice(ctx context.Context, state any, question string, options map[string]string) (*Answer, error) {
	const key = "q"
	r, err := c.Decide(ctx, state, map[string]Question{key: Choice(question, options)})
	if err != nil {
		return nil, err
	}
	a, ok := r.Answers[key]
	if !ok {
		return nil, errors.New("jevos: answer missing in response")
	}
	return &a, nil
}

// AskScore asks one score question; levels are ordered from lowest to highest.
// The result has Score, Legend, Probabilities and Confidence; use Level()
// for the nearest label.
func (c *Client) AskScore(ctx context.Context, state any, question string, levels ...string) (*Answer, error) {
	const key = "q"
	r, err := c.Decide(ctx, state, map[string]Question{key: Score(question, levels...)})
	if err != nil {
		return nil, err
	}
	a, ok := r.Answers[key]
	if !ok {
		return nil, errors.New("jevos: answer missing in response")
	}
	return &a, nil
}

// Yes asks a yes/no question and reports whether P(yes) >= threshold.
func (c *Client) Yes(ctx context.Context, state any, question string, threshold float64) (bool, error) {
	p, err := c.Ask(ctx, state, question)
	if err != nil {
		return false, err
	}
	return p >= threshold, nil
}

// Health calls GET /health and returns the raw JSON body.
func (c *Client) Health(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

// Get returns the answer for the named question.
func (r *Response) Get(name string) (Answer, bool) {
	a, ok := r.Answers[name]
	return a, ok
}

// Query builds a request with several named questions about one state.
//
//	res, err := c.Query(text).
//		YesNo("refund", "Should this customer get a refund?").
//		Choice("team", "Which team?", map[string]string{"billing": "payments", "tech": "broken product"}).
//		Score("anger", "How angry is the customer?", "calm", "annoyed", "angry", "furious").
//		Do(ctx)
type Query struct {
	c     *Client
	state any
	qs    map[string]Question
	err   error
}

// Query starts a Query for the given state (a string or any JSON-serializable value).
func (c *Client) Query(state any) *Query {
	return &Query{c: c, state: state, qs: map[string]Question{}}
}

func (q *Query) add(name string, question Question) *Query {
	if _, dup := q.qs[name]; dup && q.err == nil {
		q.err = fmt.Errorf("jevos: duplicate question name %q", name)
	}
	q.qs[name] = question
	return q
}

// YesNo adds a yes/no question.
func (q *Query) YesNo(name, instructions string) *Query {
	return q.add(name, YesNo(instructions))
}

// Choice adds a multiple-choice question.
func (q *Query) Choice(name, instructions string, options map[string]string) *Query {
	return q.add(name, Choice(instructions, options))
}

// Score adds a score question; levels go from lowest to highest.
func (q *Query) Score(name, instructions string, levels ...string) *Query {
	return q.add(name, Score(instructions, levels...))
}

// Do sends the request.
func (q *Query) Do(ctx context.Context) (*Response, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.c.Decide(ctx, q.state, q.qs)
}
