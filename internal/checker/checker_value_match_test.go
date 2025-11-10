package checker

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValueMatchChecker_Check_Ignore(t *testing.T) {
	cli := &http.Client{}
	c := &valueMatchChecker{
		cacheExpire:   100 * time.Millisecond,
		JsonRpcCaller: JsonRpcCaller{},
		cli:           cli,
	}

	url := "http://rpc.example"
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "0x1"}},
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

func TestValueMatchChecker_Check_CacheHit_Success(t *testing.T) {
	cli := &http.Client{}
	c := &valueMatchChecker{
		cacheExpire:   100 * time.Millisecond,
		JsonRpcCaller: JsonRpcCaller{},
		cli:           cli,
	}

	url := "http://rpc.example/cache"
	payload := `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       payload,
		Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "OK"}},
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

func TestValueMatchChecker_Check_CacheHit_Mismatch(t *testing.T) {
	cli := &http.Client{}
	c := &valueMatchChecker{
		cacheExpire:   100 * time.Millisecond,
		JsonRpcCaller: JsonRpcCaller{},
		cli:           cli,
	}

	url := "http://rpc.example/cache-mismatch"
	payload := `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       payload,
		Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "EXPECTED"}},
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
	caches[key] = NewTimedCache(checkCacheValue{"result": "ACTUAL"}, time.Second)

	ret, err := c.Check("1", []string{url}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret[url] {
		t.Fatalf("expected false due to mismatch, got %v", ret[url])
	}
}

func TestValueMatchChecker_Check_CallError_ReturnsFalseNoError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cli := &http.Client{}
	c := &valueMatchChecker{
		cacheExpire:   100 * time.Millisecond,
		JsonRpcCaller: JsonRpcCaller{},
		cli:           cli,
	}

	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "anything"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{ts.URL}, cond, caches)
	if err != nil {
		t.Fatalf("expected no error from Check, got: %v", err)
	}
	if ret[ts.URL] {
		t.Fatalf("expected false due to call error, got %v", ret[ts.URL])
	}
}

func TestValueMatchChecker_Check_EqualAndNotEqual(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"abc"}`))
	}))
	defer ts.Close()

	cli := &http.Client{}
	c := &valueMatchChecker{
		cacheExpire:   100 * time.Millisecond,
		JsonRpcCaller: JsonRpcCaller{},
		cli:           cli,
	}
	caches := CheckCaches{}

	condEq := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "xyz"}},
	}
	retEq, err := c.Check("1", []string{ts.URL}, condEq, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retEq[ts.URL] {
		t.Fatalf("expected false for '=' mismatch, got %v", retEq[ts.URL])
	}

	condNe := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "!=", Key: "result", Value: "xyz"}},
	}
	retNe, err := c.Check("1", []string{ts.URL}, condNe, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retNe[ts.URL] {
		t.Fatalf("expected true for '!=' when value differs, got %v", retNe[ts.URL])
	}

	condNeFail := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Payload:       `{"jsonrpc":"2.0","method":"any","params":[],"id":1}`,
		Matchers:      []Matcher{{MatchType: "!=", Key: "result", Value: "abc"}},
	}
	retNeFail, err := c.Check("1", []string{ts.URL}, condNeFail, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retNeFail[ts.URL] {
		t.Fatalf("expected false for '!=' when value equals, got %v", retNeFail[ts.URL])
	}
}
