package checker

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBlockHeightChecker_ValidCondition_EmptyPayload(t *testing.T) {
	c := &blockHeightChecker{}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       "",
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}
	if err := c.ValidCondition(cond); err == nil {
		t.Fatalf("expected error for empty payload, got nil")
	}
}

func TestBlockHeightChecker_ValidCondition_InvalidMatchers(t *testing.T) {
	c := &blockHeightChecker{}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers: []Matcher{
			{MatchType: "=", Key: "result", Value: "1"},
			{MatchType: ">", Key: "result", Value: "1"},
		},
	}
	if err := c.ValidCondition(cond); err == nil {
		t.Fatalf("expected error for invalid matchers, got nil")
	}
}

func TestBlockHeightChecker_parseHeight(t *testing.T) {
	c := &blockHeightChecker{}
	dec, err := c.parseHeight("42")
	if err != nil || dec != 42 {
		t.Fatalf("expected 42, got %d (err=%v)", dec, err)
	}
	hex, err := c.parseHeight("0x2a")
	if err != nil || hex != 42 {
		t.Fatalf("expected 42 from hex, got %d (err=%v)", hex, err)
	}
}

func TestBlockHeightChecker_Check_Ignore(t *testing.T) {
	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks:    make(map[string]int64),
	}
	url := "http://rpc.example"
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
		Ignore:        []string{url},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{url}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[url] {
		t.Fatalf("expected true for ignored url, got %v", ret[url])
	}
}

func TestBlockHeightChecker_Check_CallError_ReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks:    make(map[string]int64),
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{ts.URL}, cond, caches)
	if err != nil {
		t.Fatalf("did not expect error from Check, got %v", err)
	}
	if ret[ts.URL] {
		t.Fatalf("expected false due to call error, got %v", ret[ts.URL])
	}
}

func TestBlockHeightChecker_Check_Success_WithTolerance_LessEqual(t *testing.T) {
	ts100 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"0x64"}`)) // 100
	}))
	defer ts100.Close()

	ts98 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"0x62"}`)) // 98
	}))
	defer ts98.Close()

	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks:    make(map[string]int64),
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "0x2"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("chainA", []string{ts100.URL, ts98.URL}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[ts100.URL] || !ret[ts98.URL] {
		t.Fatalf("expected both true under <= tolerance, got %v", ret)
	}
}

func TestBlockHeightChecker_Check_Success_WithTolerance_LessOnly(t *testing.T) {
	ts100 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":"0x64"}`)) // 100
	}))
	defer ts100.Close()

	ts98 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":"0x62"}`)) // 98
	}))
	defer ts98.Close()

	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks:    make(map[string]int64),
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "<", Key: "result", Value: "0x2"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("chainB", []string{ts100.URL, ts98.URL}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret[ts98.URL] {
		t.Fatalf("expected false for 98 when diff equals tolerance 2 under '<', got true")
	}
	if !ret[ts100.URL] {
		t.Fatalf("expected true for 100 when diff 0 < 2 under '<', got false")
	}
}

func TestBlockHeightChecker_Check_UsesLastBlocks(t *testing.T) {
	ts10 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":"0xa"}`)) // 10
	}))
	defer ts10.Close()

	ts8 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":"0x8"}`)) // 8
	}))
	defer ts8.Close()

	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks: map[string]int64{
			"chainC": 20,
		},
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "12"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("chainC", []string{ts10.URL, ts8.URL}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[ts10.URL] || !ret[ts8.URL] {
		t.Fatalf("expected both true using lastBlocks, got %v", ret)
	}
}

func TestBlockHeightChecker_getHeight_NoValue(t *testing.T) {
	c := &blockHeightChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
		lastBlocks:    make(map[string]int64),
	}
	url := "http://rpc.example/no-value"
	payload := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_BLOCK_HEIGHT,
		Payload:       payload,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}
	caches := CheckCaches{}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("unexpected error building request: %v", err)
	}
	key, err := caches.makeKey(req)
	if err != nil {
		t.Fatalf("unexpected error making cache key: %v", err)
	}
	caches[key] = NewTimedCache(checkCacheValue{"foo": "bar"}, time.Second)

	h, err := c.getHeight(url, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != -1 {
		t.Fatalf("expected -1 for '<no value>', got %d", h)
	}
}
