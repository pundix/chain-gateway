package checker

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/golang/protobuf/jsonpb"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type checkStrategy string

const (
	CHECK_STRATEGY_VALUE_MATCH       checkStrategy = "ValueMatch"
	CHECK_STRATEGY_BLOCK_HEIGHT      checkStrategy = "BlockHeight"
	CHECK_STRATEGY_GRPC_BLOCK_HEIGHT checkStrategy = "GrpcBlockHeight"
	CHECK_STRATEGY_SIMPLE            checkStrategy = "Simple"
	CHECK_STRATEGY_MANUAL            checkStrategy = "Manual"
)

type HealthCheckCondition struct {
	Ignore        []string      `json:"ignore,omitempty"`
	CheckStrategy checkStrategy `json:"checkStrategy"`
	Payload       string        `json:"payload,omitempty"`
	Matchers      []Matcher     `json:"matchers"`
}

func (c *HealthCheckCondition) ignore(url string) bool {
	return lo.Contains(c.Ignore, url)
}

type HealthCheckConditionList []*HealthCheckCondition

func (cl HealthCheckConditionList) Check(checker HealthChecker, chainId string, urls []string, caches CheckCaches) (map[string]bool, error) {
	ret := make(map[string]bool, len(urls))
	for _, condition := range cl {
		urls = lo.Filter(urls, func(u string, _ int) bool {
			if valid, ok := ret[u]; ok {
				return valid
			}
			return true
		})
		curRet, err := checker.Check(chainId, urls, condition, caches)
		if err != nil {
			return nil, err
		}
		// sync result
		for k, valid := range curRet {
			ret[k] = valid
		}
	}
	return ret, nil
}

type Matcher struct {
	MatchType string `json:"matchType"`
	Key       string `json:"key,omitempty"`
	Value     string `json:"value"`
}

type HealthChecker interface {
	Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error)
	ValidCondition(condition *HealthCheckCondition) error
}

type checkCacheValue map[string]interface{}

type CheckCaches map[string]*TimedCache[checkCacheValue]

// package-level mutex for protecting concurrent access to checkCaches (map)
var checkCachesMu sync.RWMutex

func (cc CheckCaches) match(req *http.Request) (*TimedCache[checkCacheValue], error) {
	key, err := cc.makeKey(req)
	if err != nil {
		return nil, err
	}
	checkCachesMu.RLock()
	resp, ok := cc[key]
	checkCachesMu.RUnlock()
	if ok {
		return resp, nil
	}
	noNull := NewTimedCache(checkCacheValue{}, time.Second)
	noNull.Expire()
	return noNull, nil
}

func (cc CheckCaches) makeKey(req *http.Request) (string, error) {
	body, err := req.GetBody()
	if err != nil {
		return "", err
	}
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	return req.URL.String() + string(bodyBytes), nil
}

func (cc CheckCaches) put(req *http.Request, value checkCacheValue, expire time.Duration) error {
	key, err := cc.makeKey(req)
	if err != nil {
		return err
	}
	// write-lock while touching the underlying map
	checkCachesMu.Lock()
	cache, ok := cc[key]
	if !ok {
		cache = NewTimedCache(value, expire)
		cc[key] = cache
		checkCachesMu.Unlock()
		return nil
	}
	checkCachesMu.Unlock()
	cache.Set(value)
	return nil
}

type JsonRpcCaller struct {
}

func (c *JsonRpcCaller) Call(cli *http.Client, req *http.Request) (map[string]interface{}, error) {
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	resp, err := cli.Do(req)
	if err != nil {
		log.Printf("fail to call, url: %s, err: %s\n", req.URL.String(), err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status code, url: %s , code: %d\n", req.URL.String(), resp.StatusCode)
		return nil, fmt.Errorf("unexpected status code, url: %s , code: %d", req.URL.String(), resp.StatusCode)
	}
	defer resp.Body.Close()

	var ret map[string]interface{}
	return ret, json.NewDecoder(resp.Body).Decode(&ret)
}

type TimedCache[T any] struct {
	mu     sync.RWMutex
	value  T
	time   time.Time
	expire time.Duration
}

func NewTimedCache[T any](value T, expire time.Duration) *TimedCache[T] {
	return &TimedCache[T]{value: value, time: time.Now(), expire: expire}
}

func (c *TimedCache[T]) Get() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.time) > c.expire {
		return c.value, false
	}
	return c.value, true
}

func (c *TimedCache[T]) Set(value T) {
	c.mu.Lock()
	c.value = value
	c.time = time.Now()
	c.mu.Unlock()
}

func (c *TimedCache[T]) Expire() {
	c.mu.Lock()
	c.time = time.Now().Add(-c.expire)
	c.mu.Unlock()
}

type TextTemplate string

func (tt TextTemplate) Parse(values map[string]interface{}) (ret string, err error) {
	var tmpl *template.Template
	if tmpl, err = template.New("unit").Parse(string(tt)); err != nil {
		return
	}

	var b []byte
	buf := bytes.Buffer{}
	if err = tmpl.Execute(&buf, values); err != nil {
		return
	}
	if b, err = io.ReadAll(&buf); err != nil {
		return
	}
	return string(b), nil
}

type checkResult struct {
	url   string
	valid bool
	err   error
}

type heightResult struct {
	url    string
	height int64
	err    error
}

type GrpcCaller struct{}

func (c *GrpcCaller) Call(url, protoset, service, method string) (map[string]interface{}, error) {
	var creds grpc.DialOption
	if strings.Contains(url, "443") {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	} else {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	cc, err := grpc.NewClient(url, creds)
	if err != nil {
		return nil, err
	}
	defer cc.Close()
	data, err := os.ReadFile(protoset)
	if err != nil {
		return nil, err
	}

	var fs descriptorpb.FileDescriptorSet
	if err = proto.Unmarshal(data, &fs); err != nil {
		return nil, err
	}

	fd, _ := desc.CreateFileDescriptorFromSet(&fs)
	md := fd.FindService(fmt.Sprintf("%s.%s", fd.GetPackage(), service)).FindMethodByName(method)
	req := dynamic.NewMessage(md.GetInputType())
	stub := grpcdynamic.NewStub(cc)
	return retry.DoWithData(func() (map[string]interface{}, error) {
		reply, err := stub.InvokeRpc(context.Background(), md, req)
		if err != nil {
			return nil, err
		}
		marshaller := jsonpb.Marshaler{}
		str, _ := marshaller.MarshalToString(reply)
		var ret map[string]interface{}
		return ret, json.Unmarshal([]byte(str), &ret)
	}, retry.Attempts(3), retry.Delay(500*time.Millisecond))
}
