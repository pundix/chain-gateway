package checker

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var grpcCallStub func(url, protoset, service, method string) (map[string]interface{}, error)

func (c *grpcBlockHeightChecker) Call(url, protoset, service, method string) (map[string]interface{}, error) {
	if grpcCallStub != nil {
		return grpcCallStub(url, protoset, service, method)
	}
	return nil, fmt.Errorf("grpcCallStub not set")
}

func TestGrpcBlockHeightChecker_GetHeight_PayloadUnmarshalError(t *testing.T) {
	c := &grpcBlockHeightChecker{
		blockHeightChecker: blockHeightChecker{
			cacheExpire: time.Second,
		},
		lastBlocks: make(map[string]int64),
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       "{not-json",
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err == nil || h != -1 {
		t.Fatalf("expected (-1, error), got (%d, %v)", h, err)
	}
}

func TestGrpcBlockHeightChecker_GetHeight_CallError(t *testing.T) {
	defer func() { grpcCallStub = nil }()

	grpcCallStub = func(url, protoset, service, method string) (map[string]interface{}, error) {
		return nil, errors.New("boom")
	}

	c := &grpcBlockHeightChecker{
		blockHeightChecker: blockHeightChecker{},
		lastBlocks:         make(map[string]int64),
	}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       `{"protoset":"unused","service":"Svc","method":"M"}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err != nil || h != -1 {
		t.Fatalf("expected (-1, nil) on call error, got (%d, %v)", h, err)
	}
}

func TestGrpcBlockHeightChecker_GetHeight_NoValue(t *testing.T) {
	defer func() { grpcCallStub = nil }()

	grpcCallStub = func(url, protoset, service, method string) (map[string]interface{}, error) {
		return map[string]interface{}{"foo": "bar"}, nil
	}

	c := &grpcBlockHeightChecker{blockHeightChecker: blockHeightChecker{}}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       `{"protoset":"unused","service":"Svc","method":"M"}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result", Value: "1"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err != nil || h != -1 {
		t.Fatalf("expected (-1, nil) for <no value>, got (%d, %v)", h, err)
	}
}

func TestGrpcBlockHeightChecker_GetHeight_RegexNoMatch(t *testing.T) {
	defer func() { grpcCallStub = nil }()

	grpcCallStub = func(url, protoset, service, method string) (map[string]interface{}, error) {
		return map[string]interface{}{"result": "abc"}, nil
	}

	c := &grpcBlockHeightChecker{blockHeightChecker: blockHeightChecker{}}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       `{"protoset":"unused","service":"Svc","method":"M"}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result|^(0x[0-9a-fA-F]+)$", Value: "2"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err != nil || h != -1 {
		t.Fatalf("expected (-1, nil) for regex no match, got (%d, %v)", h, err)
	}
}

func TestGrpcBlockHeightChecker_GetHeight_ParseHexSuccess(t *testing.T) {
	defer func() { grpcCallStub = nil }()

	grpcCallStub = func(url, protoset, service, method string) (map[string]interface{}, error) {
		return map[string]interface{}{"result": "0x2a"}, nil
	}

	c := &grpcBlockHeightChecker{blockHeightChecker: blockHeightChecker{}}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       `{"protoset":"unused","service":"Svc","method":"M"}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "result|^(0x[0-9a-fA-F]+)$", Value: "0x5"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != 42 {
		t.Fatalf("expected height 42, got %d", h)
	}
}

func TestGrpcBlockHeightChecker_GetHeight_ParseDecimalSuccess(t *testing.T) {
	defer func() { grpcCallStub = nil }()

	grpcCallStub = func(url, protoset, service, method string) (map[string]interface{}, error) {
		return map[string]interface{}{"height": "42"}, nil
	}

	c := &grpcBlockHeightChecker{blockHeightChecker: blockHeightChecker{}}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_GRPC_BLOCK_HEIGHT,
		Payload:       `{"protoset":"unused","service":"Svc","method":"M"}`,
		Matchers:      []Matcher{{MatchType: "<=", Key: "height|([0-9]+)", Value: "2"}},
	}

	h, err := c.getHeight("localhost:9090", cond, CheckCaches{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != 42 {
		t.Fatalf("expected height 42, got %d", h)
	}
}
