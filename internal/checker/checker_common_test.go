package checker

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

type fakeHealthChecker struct {
	validErr    error
	checkErr    error
	ret         map[string]bool
	validCalled int
	checkCalled int
}

func (f *fakeHealthChecker) Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	f.checkCalled++
	return f.ret, f.checkErr
}

func (f *fakeHealthChecker) ValidCondition(condition *HealthCheckCondition) error {
	f.validCalled++
	return f.validErr
}

func TestCommonChecker_Check_EmptyURLs(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 50 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Matchers:      []Matcher{{MatchType: "=", Key: "x", Value: "y"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ret) != 0 {
		t.Fatalf("expected empty result, got %v", ret)
	}
}

func TestCommonChecker_Check_Create_ValueMatch(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
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
		t.Fatalf("expected url to be valid (ignored), got %v", ret)
	}

	created, ok := c.checkers[CHECK_STRATEGY_VALUE_MATCH]
	if !ok {
		t.Fatalf("expected checker to be cached in common checker")
	}
	vmc, ok := created.(*valueMatchChecker)
	if !ok {
		t.Fatalf("expected valueMatchChecker, got %#v", created)
	}
	if vmc.cli != c.Cli {
		t.Fatalf("expected checker cli to equal common cli")
	}
	if vmc.cacheExpire != c.CacheExpire {
		t.Fatalf("expected checker cacheExpire to equal common cacheExpire")
	}
}

func TestCommonChecker_Check_Create_BlockHeight(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
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
		t.Fatalf("expected url to be valid (ignored), got %v", ret)
	}

	created, ok := c.checkers[CHECK_STRATEGY_BLOCK_HEIGHT]
	if !ok {
		t.Fatalf("expected checker to be cached in common checker")
	}
	bhc, ok := created.(*blockHeightChecker)
	if !ok {
		t.Fatalf("expected blockHeightChecker, got %#v", created)
	}
	if bhc.cli != c.Cli {
		t.Fatalf("expected checker cli to equal common cli")
	}
	if bhc.cacheExpire != c.CacheExpire {
		t.Fatalf("expected checker cacheExpire to equal common cacheExpire")
	}
	if bhc.lastBlocks == nil {
		t.Fatalf("expected lastBlocks to be initialized")
	}
}

func TestCommonChecker_Check_Create_Simple_WithCacheHit(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
	}
	url := "http://rpc.example"
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
		t.Fatalf("expected url to be valid due to cache hit, got %v", ret)
	}

	created, ok := c.checkers[CHECK_STRATEGY_SIMPLE]
	if !ok {
		t.Fatalf("expected checker to be cached in common checker")
	}
	sc, ok := created.(*simpleChecker)
	if !ok {
		t.Fatalf("expected simpleChecker, got %#v", created)
	}
	if sc.cli != c.Cli {
		t.Fatalf("expected checker cli to equal common cli")
	}
	if sc.cacheExpire != c.CacheExpire {
		t.Fatalf("expected checker cacheExpire to equal common cacheExpire")
	}
}

func TestCommonChecker_Check_Create_Manual(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
	}
	urlGood := "http://foo:8545"
	urlBad := "http://bar:8545"
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers: []Matcher{
			{MatchType: "=", Key: "unused", Value: "foo"},
		},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{urlGood, urlBad}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[urlGood] {
		t.Fatalf("expected urlGood to be valid, got %v", ret[urlGood])
	}
	if ret[urlBad] {
		t.Fatalf("expected urlBad to be invalid, got %v", ret[urlBad])
	}

	created, ok := c.checkers[CHECK_STRATEGY_MANUAL]
	if !ok {
		t.Fatalf("expected checker to be cached in common checker")
	}
	if _, ok := created.(*manualChecker); !ok {
		t.Fatalf("expected manualChecker, got %#v", created)
	}
}

func TestCommonChecker_Check_UnsupportedStrategy(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: checkStrategy("Unknown"),
	}
	caches := CheckCaches{}

	_, err := c.Check("1", []string{"http://rpc.example"}, cond, caches)
	if err == nil {
		t.Fatalf("expected error for unsupported strategy, got nil")
	}
}

func TestCommonChecker_Check_ValidConditionError_ValueMatch(t *testing.T) {
	c := &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Matchers:      []Matcher{{MatchType: "<", Key: "result", Value: "0x1"}},
	}
	caches := CheckCaches{}

	_, err := c.Check("1", []string{"http://rpc.example"}, cond, caches)
	if err == nil {
		t.Fatalf("expected error from ValidCondition, got nil")
	}
	if _, ok := c.checkers[CHECK_STRATEGY_VALUE_MATCH]; !ok {
		t.Fatalf("expected checker to be created even when validation fails")
	}
}

func TestCommonChecker_Check_UseExistingChecker(t *testing.T) {
	fake := &fakeHealthChecker{
		ret: map[string]bool{"http://rpc.example": true},
	}
	c := &CommonChecker{
		checkers: map[checkStrategy]HealthChecker{
			CHECK_STRATEGY_VALUE_MATCH: fake,
		},
		Cli:         &http.Client{},
		CacheExpire: 100 * time.Millisecond,
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
		Matchers:      []Matcher{{MatchType: "=", Key: "x", Value: "y"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{"http://rpc.example"}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret["http://rpc.example"] {
		t.Fatalf("expected true, got %v", ret)
	}
	if fake.validCalled != 1 || fake.checkCalled != 1 {
		t.Fatalf("expected ValidCondition and Check to be called once, got valid=%d check=%d", fake.validCalled, fake.checkCalled)
	}
	if c.checkers[CHECK_STRATEGY_VALUE_MATCH] != fake {
		t.Fatalf("expected existing checker to be reused, but got replaced")
	}
}
