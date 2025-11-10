package checker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
)

type CommonChecker struct {
	checkers map[checkStrategy]HealthChecker
	Cli      *http.Client
	// caches      checkCaches
	CacheExpire time.Duration
	mu          sync.RWMutex
}

func New(cli *http.Client, cacheExpire time.Duration) HealthChecker {
	if cli == nil {
		cli = http.DefaultClient
	}
	return &CommonChecker{
		checkers:    map[checkStrategy]HealthChecker{},
		Cli:         cli,
		CacheExpire: cacheExpire,
	}
}

func (c *CommonChecker) ValidCondition(condition *HealthCheckCondition) error {
	return nil
}

func (c *CommonChecker) Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	if len(urls) == 0 {
		return make(map[string]bool, len(urls)), nil
	}
	c.mu.RLock()
	checker, ok := c.checkers[condition.CheckStrategy]
	c.mu.RUnlock()
	if !ok {
		switch condition.CheckStrategy {
		case CHECK_STRATEGY_VALUE_MATCH:
			checker = &valueMatchChecker{
				cacheExpire:   c.CacheExpire,
				JsonRpcCaller: JsonRpcCaller{},
				cli:           c.Cli,
			}
		case CHECK_STRATEGY_BLOCK_HEIGHT:
			checker = &blockHeightChecker{
				cacheExpire:   c.CacheExpire,
				JsonRpcCaller: JsonRpcCaller{},
				cli:           c.Cli,
				lastBlocks:    make(map[string]int64),
			}
		case CHECK_STRATEGY_SIMPLE:
			checker = &simpleChecker{
				cacheExpire:   c.CacheExpire,
				JsonRpcCaller: JsonRpcCaller{},
				cli:           c.Cli,
			}
		case CHECK_STRATEGY_MANUAL:
			checker = &manualChecker{}
		case CHECK_STRATEGY_GRPC_BLOCK_HEIGHT:
			grpcChecker := &grpcBlockHeightChecker{
				lastBlocks: make(map[string]int64),
				GrpcCaller: GrpcCaller{},
			}
			grpcChecker.blockHeightChecker = blockHeightChecker{
				cacheExpire:   c.CacheExpire,
				JsonRpcCaller: JsonRpcCaller{},
				cli:           c.Cli,
				lastBlocks:    make(map[string]int64),
				// inject grpc version getHeightFn
				getHeightFn: grpcChecker.getHeight,
			}
			checker = grpcChecker
		default:
			return nil, fmt.Errorf("check strategy %s not supported", condition.CheckStrategy)
		}
		c.mu.Lock()
		c.checkers[condition.CheckStrategy] = checker
		c.mu.Unlock()
	}
	if err := checker.ValidCondition(condition); err != nil {
		return nil, err
	}
	return checker.Check(chainId, urls, condition, caches)
}

type valueMatchChecker struct {
	cacheExpire time.Duration
	JsonRpcCaller
	cli *http.Client
}

func (c *valueMatchChecker) Check(_ string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	resultCh := make(chan checkResult, len(urls))
	// defer close(resultCh)
	for _, url := range urls {
		if condition.ignore(url) {
			resultCh <- checkResult{url: url, valid: true}
			continue
		}
		go func(u string) {
			valid, err := c.check(u, condition, caches)
			resultCh <- checkResult{url: u, valid: valid, err: err}
		}(url)
	}

	ret := make(map[string]bool, len(urls))
	for range urls {
		r := <-resultCh
		if r.err != nil {
			log.Printf("check url %s error: %s\n", r.url, r.err.Error())
			return nil, r.err
		}
		ret[r.url] = r.valid
	}
	return ret, nil
}

func (c *valueMatchChecker) check(url string, condition *HealthCheckCondition, caches CheckCaches) (bool, error) {
	checkResult := false
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(condition.Payload))
	if err != nil {
		return checkResult, err
	}
	cache, err := caches.match(req)
	if err != nil {
		return checkResult, err
	}
	value, ok := cache.Get()
	if !ok {
		value, err = c.Call(c.cli, req)
		if err != nil {
			// log.Printf("check url %s error: %s\n", url, err.Error())
			return checkResult, nil
		}
		if value == nil || value["error"] != nil {
			if value["error"] != nil {
				errorInfo, ok := value["error"].(map[string]interface{})
				if ok {
					log.Printf("checkStrategy: %s, check url %s , code: %.0f, error: %s\n", condition.CheckStrategy, url, errorInfo["code"].(float64), errorInfo["message"].(string))
				}
			}
			return checkResult, nil
		}
		if err = caches.put(req, value, c.cacheExpire); err != nil {
			return checkResult, err
		}
	} else {
		log.Printf("hit cache for %s, checkStrategy: %s\n", req.URL.String(), condition.CheckStrategy)
	}

	for _, matcher := range condition.Matchers {
		val, err := TextTemplate(fmt.Sprintf("{{.%s}}", matcher.Key)).Parse(value)
		if err != nil {
			return checkResult, err
		}
		checkResult = val == matcher.Value
		if matcher.MatchType == "!=" {
			checkResult = !checkResult
		}
		if !checkResult {
			log.Printf("checkStrategy: %s, %s %s %s, result: %t for %s\n", condition.CheckStrategy, val, matcher.MatchType, matcher.Value, checkResult, url)
			break
		}
	}
	return checkResult, nil
}

func (c *valueMatchChecker) ValidCondition(condition *HealthCheckCondition) error {
	condition.Matchers = lo.Filter(condition.Matchers, func(m Matcher, _ int) bool {
		return m.MatchType == "=" || m.MatchType == "!="
	})
	if len(condition.Matchers) == 0 {
		return errors.New("invalid or empty matchers")
	}
	return nil
}

type blockHeightChecker struct {
	JsonRpcCaller
	cli         *http.Client
	cacheExpire time.Duration
	lastBlocks  map[string]int64
	getHeightFn func(url string, condition *HealthCheckCondition, caches CheckCaches) (int64, error)
}

func (c *blockHeightChecker) Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	ret := make(map[string]bool, len(urls))

	heightMap := make(map[string]int64, len(urls))
	heights := make([]int64, len(urls))
	matcher := condition.Matchers[0]
	if c.getHeightFn == nil {
		c.getHeightFn = c.getHeight
	}

	heightCh := make(chan heightResult, len(urls))
	// defer close(heightCh)
	for _, url := range urls {
		if condition.ignore(url) {
			heightCh <- heightResult{url: url, height: 0}
			continue
		}
		go func(u string) {
			h, e := c.getHeightFn(u, condition, caches)
			heightCh <- heightResult{url: u, height: h, err: e}
		}(url)
	}

	for range urls {
		r := <-heightCh
		if r.err != nil {
			log.Printf("checkStrategy: %s, check url %s error: %s\n", condition.CheckStrategy, r.url, r.err.Error())
			return nil, r.err
		}
		switch r.height {
		case -1:
			ret[r.url] = false
		case 0:
			ret[r.url] = true
		default:
			heightMap[r.url] = r.height
			heights = append(heights, r.height)
		}
	}
	if len(heights) == 0 {
		return ret, nil
	}

	tolerance, err := c.parseHeight(matcher.Value)
	if err != nil {
		return nil, err
	}
	max := lo.Max(heights)
	lastBlock, ok := c.lastBlocks[chainId]
	if ok && lastBlock > max {
		max = lastBlock
	} else {
		// update
		c.lastBlocks[chainId] = max
	}

	for url, height := range heightMap {
		checkResult := false
		if matcher.MatchType == "<" && max-height < tolerance {
			checkResult = true
		}
		if matcher.MatchType == "<=" && max-height <= tolerance {
			checkResult = true
		}
		ret[url] = checkResult
		if !checkResult {
			log.Printf("checkStrategy: %s, %d - %d %s %d, result: %t for %s\n", condition.CheckStrategy, max, height, matcher.MatchType, tolerance, checkResult, url)
		}
	}
	return ret, nil
}

func (c *blockHeightChecker) ValidCondition(condition *HealthCheckCondition) error {
	if condition.Payload == "" {
		return errors.New("invalid or empty payload")
	}

	condition.Matchers = lo.Filter(condition.Matchers, func(m Matcher, _ int) bool {
		return m.MatchType == "<" || m.MatchType == "<="
	})
	if len(condition.Matchers) == 0 {
		return errors.New("invalid or empty matchers")
	}
	return nil
}

func (c *blockHeightChecker) getHeight(url string, condition *HealthCheckCondition, caches CheckCaches) (int64, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(condition.Payload)))
	if err != nil {
		return -1, err
	}
	cache, err := caches.match(req)
	if err != nil {
		return -1, err
	}
	value, ok := cache.Get()
	if !ok {
		value, err = c.Call(c.cli, req)
		if err != nil {
			log.Printf("checkStrategy: %s, check url %s error: %s\n", condition.CheckStrategy, url, err.Error())
			// ret[url] = false
			// continue
			return -1, nil
		}
		if value == nil || value["error"] != nil {
			if value["error"] != nil {
				errorInfo, ok := value["error"].(map[string]interface{})
				if ok {
					log.Printf("checkStrategy: %s, check url %s , code: %.0f, error: %s\n", condition.CheckStrategy, url, errorInfo["code"].(float64), errorInfo["message"].(string))
				}
			}
			return -1, nil
		}
		if err = caches.put(req, value, c.cacheExpire); err != nil {
			return -1, err
		}
	} else {
		log.Printf("hit cache for %s, checkStrategy: %s\n", req.URL.String(), condition.CheckStrategy)
	}

	matcher := condition.Matchers[0]
	var val string
	if val, err = TextTemplate(fmt.Sprintf("{{.%s}}", matcher.Key)).Parse(value); err != nil {
		return -1, err
	}

	if val == "" || val == "<no value>" {
		return -1, nil
	}
	height, err := c.parseHeight(val)
	if err != nil {
		log.Printf("checkStrategy: %s, check url %s , error: %s\n", condition.CheckStrategy, url, err.Error())
		return -1, nil
	}
	return height, nil
}

func (c *blockHeightChecker) parseHeight(val string) (int64, error) {
	base := 10
	if strings.HasPrefix(val, "0x") {
		val = strings.TrimPrefix(val, "0x")
		base = 16
	}
	return strconv.ParseInt(val, base, 64)
}

type simpleChecker struct {
	JsonRpcCaller
	cli         *http.Client
	cacheExpire time.Duration
}

func (c *simpleChecker) check(url string, condition *HealthCheckCondition, caches CheckCaches) (bool, error) {
	if condition.ignore(url) {
		return true, nil
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(condition.Payload)))
	if err != nil {
		return false, err
	}
	cache, err := caches.match(req)
	if err != nil {
		return false, err
	}
	_, ok := cache.Get()
	if !ok {
		value, err := c.Call(c.cli, req)
		if err != nil {
			log.Printf("checkStrategy: %s, check url %s error: %s\n", condition.CheckStrategy, url, err.Error())
			return false, nil
		}
		if value == nil || value["error"] != nil {
			if value["error"] != nil {
				errorInfo, ok := value["error"].(map[string]interface{})
				if ok {
					log.Printf("checkStrategy: %s, check url %s , code: %.0f, error: %s\n", condition.CheckStrategy, url, errorInfo["code"].(float64), errorInfo["message"].(string))
				}
			}
			return false, nil
		}
		if err = caches.put(req, value, c.cacheExpire); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (c *simpleChecker) Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	ret := make(map[string]bool, len(urls))
	resultCh := make(chan checkResult, len(urls))
	// defer close(resultCh)
	for _, url := range urls {
		go func(u string) {
			valid, err := c.check(u, condition, caches)
			resultCh <- checkResult{url: u, valid: valid, err: err}
		}(url)
	}
	for range urls {
		r := <-resultCh
		if r.err != nil {
			return nil, r.err
		}
		ret[r.url] = r.valid
	}
	return ret, nil
}

func (c *simpleChecker) ValidCondition(condition *HealthCheckCondition) error {
	if condition.Payload == "" {
		return errors.New("invalid or empty payload")
	}
	return nil
}

type manualChecker struct {
}

func (c *manualChecker) Check(chainId string, urls []string, condition *HealthCheckCondition, caches CheckCaches) (map[string]bool, error) {
	ret := make(map[string]bool, len(urls))
	for _, url := range urls {
		var checkResult bool
		for _, matcher := range condition.Matchers {
			re := regexp.MustCompile(matcher.Value)
			checkResult = re.Match([]byte(url))
			if matcher.MatchType == "!=" {
				checkResult = !checkResult
			}
			if !checkResult {
				log.Printf("checkStrategy: %s, url %s filter by manual, matchType: %s, value: %s\n", condition.CheckStrategy, url, matcher.MatchType, matcher.Value)
				break
			}
		}
		ret[url] = checkResult
	}
	return ret, nil
}

func (c *manualChecker) ValidCondition(condition *HealthCheckCondition) error {
	condition.Matchers = lo.Filter(condition.Matchers, func(m Matcher, _ int) bool {
		return m.MatchType == "=" || m.MatchType == "!="
	})
	if len(condition.Matchers) == 0 {
		return errors.New("invalid or empty matchers")
	}
	return nil
}

type grpcBlockHeightChecker struct {
	blockHeightChecker
	GrpcCaller
	lastBlocks map[string]int64
}

type ConditionGrpcPayload struct {
	Protoset string `json:"protoset"`
	Service  string `json:"service"`
	Method   string `json:"method"`
}

func (c *grpcBlockHeightChecker) getHeight(url string, condition *HealthCheckCondition, _ CheckCaches) (int64, error) {
	var payload ConditionGrpcPayload
	if err := json.Unmarshal([]byte(condition.Payload), &payload); err != nil {
		return -1, err
	}
	values, err := c.Call(url, payload.Protoset, payload.Service, payload.Method)
	if err != nil {
		log.Printf("checkStrategy: %s, check url %s error: %s\n", condition.CheckStrategy, url, err.Error())
		// c.logger.Sugar().Infof("check url %s error: %s\n", url, err.Error())
		// ret[url] = false
		// continue
		return -1, nil
	}

	matcher := condition.Matchers[0]
	keys := strings.Split(matcher.Key, "|")
	parse := fmt.Sprintf("{{.%s}}", keys[0])
	var val string
	if val, err = TextTemplate(parse).Parse(values); err != nil {
		return -1, err
	}
	if val == "" || val == "<no value>" {
		// ret[url] = false
		// continue
		return -1, nil
	}
	if len(keys) > 1 {
		re := regexp.MustCompile(keys[1])
		match := re.FindStringSubmatch(val)
		if len(match) == 0 {
			// ret[url] = false
			// continue
			return -1, nil
		}
		val = match[1]
	}

	height, err := c.parseHeight(val)
	if err != nil {
		log.Printf("checkStrategy: %s, check url %s , error: %s\n", condition.CheckStrategy, url, err.Error())
		return -1, nil
	}
	return height, nil
}
