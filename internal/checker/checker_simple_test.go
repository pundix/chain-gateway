package checker

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSimpleChecker_ValidCondition_EmptyPayload(t *testing.T) {
	c := &simpleChecker{}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       "",
	}
	if err := c.ValidCondition(cond); err == nil {
		t.Fatalf("expected error for empty payload, got nil")
	}
}

func TestSimpleChecker_ValidCondition_OK(t *testing.T) {
	c := &simpleChecker{}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`,
	}
	if err := c.ValidCondition(cond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSimpleChecker_Check_CacheHit(t *testing.T) {
	c := &simpleChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
	}
	url := "http://rpc.example/cache"
	payload := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       payload,
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
	caches[key] = NewTimedCache(checkCacheValue{"result": "OK"}, time.Second)

	ret, err := c.Check("1", []string{url}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[url] {
		t.Fatalf("expected true from cache hit, got %v", ret[url])
	}
}

func TestSimpleChecker_Check_CallError_ReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := &simpleChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`,
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

func TestSimpleChecker_Check_ValueHasError_ReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":-32603,"message":"oops"}}`))
	}))
	defer ts.Close()

	c := &simpleChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`,
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{ts.URL}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret[ts.URL] {
		t.Fatalf("expected false when value contains error, got %v", ret[ts.URL])
	}
}

func TestSimpleChecker_Check_SuccessAndCachesPut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"Client/v1"}`))
	}))
	defer ts.Close()

	c := &simpleChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
	}
	payload := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       payload,
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{ts.URL}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[ts.URL] {
		t.Fatalf("expected true for success response, got %v", ret[ts.URL])
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("unexpected error building request: %v", err)
	}
	key, err := caches.makeKey(req)
	if err != nil {
		t.Fatalf("unexpected error making cache key: %v", err)
	}
	cache, ok := caches[key]
	if !ok {
		t.Fatalf("expected cache to be created")
	}
	if _, hit := cache.Get(); !hit {
		t.Fatalf("expected cache hit after successful call")
	}
}

func TestSimpleChecker_Check_MultipleURLs_Mixed(t *testing.T) {
	tsOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"OK"}`))
	}))
	defer tsOK.Close()
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tsErr.Close()

	urlCache := "http://rpc.example/cache-mixed"
	payload := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`
	caches := CheckCaches{}
	req, err := http.NewRequest(http.MethodPost, urlCache, bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("unexpected error building request: %v", err)
	}
	key, err := caches.makeKey(req)
	if err != nil {
		t.Fatalf("unexpected error making cache key: %v", err)
	}
	caches[key] = NewTimedCache(checkCacheValue{"result": "OK"}, time.Second)

	c := &simpleChecker{
		JsonRpcCaller: JsonRpcCaller{},
		cli:           &http.Client{},
		cacheExpire:   100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_SIMPLE,
		Payload:       payload,
	}

	ret, err := c.Check("1", []string{tsOK.URL, tsErr.URL, urlCache}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[tsOK.URL] {
		t.Fatalf("expected true for success url, got %v", ret[tsOK.URL])
	}
	if ret[tsErr.URL] {
		t.Fatalf("expected false for error url, got %v", ret[tsErr.URL])
	}
	if !ret[urlCache] {
		t.Fatalf("expected true for cache hit url, got %v", ret[urlCache])
	}
}
