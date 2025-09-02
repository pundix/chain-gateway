package checker

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	pkg_db "github.com/pundix/chain-gateway/pkg/db"
	"github.com/pundix/chain-gateway/pkg/types"
	"github.com/pundix/chain-gateway/pkg/upstream"
	"github.com/syumai/workers/cloudflare/fetch"
)

type checkStrategy string

const (
	CHECK_STRATEGY_VALUE_MATCH  checkStrategy = "ValueMatch"
	CHECK_STRATEGY_BLOCK_HEIGHT checkStrategy = "BlockHeight"
	CHECK_STRATEGY_SIMPLE       checkStrategy = "Simple"
	CHECK_STRATEGY_MANUAL       checkStrategy = "Manual"
)

var LAST_BLOCKS map[string]*blockHeight

type blockHeight struct {
	action string
	Value  int64
}

var FETCH_CACHE = newCache()

type cache struct {
	fetchCache map[string]map[string]interface{}
}

func newCache() *cache {
	return &cache{
		fetchCache: make(map[string]map[string]interface{}),
	}
}

func (c *cache) match(req *fetch.Request) (map[string]interface{}, error) {
	key, err := c.makeKey(req)
	if err != nil {
		return nil, err
	}
	if resp, ok := c.fetchCache[key]; ok {
		log.Println("hit cache for ", req.URL.String())
		return resp, nil
	}
	return nil, nil
}

func (c *cache) put(req *fetch.Request, resp map[string]interface{}) error {
	key, err := c.makeKey(req)
	if err != nil {
		return err
	}
	c.fetchCache[key] = resp
	return nil
}

func (c *cache) makeKey(req *fetch.Request) (string, error) {
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

type checker interface {
	check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error)
}

func (cs checkStrategy) check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error) {
	var checker checker
	switch condition.CheckStrategy {
	case CHECK_STRATEGY_VALUE_MATCH:
		checker = &valueMatchChecker{}
	case CHECK_STRATEGY_BLOCK_HEIGHT:
		checker = &blockHeightChecker{}
	case CHECK_STRATEGY_SIMPLE:
		checker = &simpleChecker{}
	case CHECK_STRATEGY_MANUAL:
		checker = &manualChecker{}
	}
	if checker == nil {
		return nil, errors.New("invalid check strategy")
	}
	return checker.check(chainId, urls, condition)
}

type HealthCheckCondition struct {
	Ignore        []string      `json:"ignore"`
	CheckStrategy checkStrategy `json:"checkStrategy"`
	Payload       string        `json:"payload"`
	Matchers      []Matcher     `json:"matchers"`
	reCache       map[string]*regexp.Regexp
}

func (c HealthCheckCondition) getRegexp(val string) *regexp.Regexp {
	if c.reCache == nil {
		c.reCache = make(map[string]*regexp.Regexp)
	}
	rex, ok := c.reCache[val]
	if !ok {
		rex = regexp.MustCompile(val)
		c.reCache[val] = rex
	}
	return rex
}

func (c HealthCheckCondition) ignore(url string) bool {
	for _, val := range c.Ignore {
		if c.getRegexp(val).Match([]byte(url)) {
			return true
		}
	}
	return false
}

func (c *HealthCheckCondition) Check(chainId string, url []string) (map[string]bool, error) {
	return c.CheckStrategy.check(chainId, url, c)
}

type HealthCheckConditionList []*HealthCheckCondition

func (cl HealthCheckConditionList) Check(wg *sync.WaitGroup, checkResult chan *pkg_db.ReadyUpstream, checkUpstream *pkg_db.Upstream) {
	defer wg.Done()

	urls := types.Rpc(checkUpstream.Rpc).GetUrls()
	var ret map[string]bool
	for _, condition := range cl {
		if ret != nil {
			urls = types.NewArrayStream(urls).Filter(func(url string) bool {
				return ret[url]
			}).Collect()
		} else {
			ret = make(map[string]bool, len(urls))
		}
		currRet, err := condition.Check(checkUpstream.ChainID, urls)
		if err != nil {
			log.Printf("check upstream by %s/%s error: %s\n", checkUpstream.Source, checkUpstream.ChainID, err.Error())
			return
		}
		// sync result
		for k, v := range currRet {
			ret[k] = v
		}
	}

	urls = types.NewArrayStream(urls).Filter(func(url string) bool {
		return ret[url]
	}).Collect()

	checkUpstream.Rpc = strings.Join(urls, ",")
	result := &pkg_db.ReadyUpstream{
		ChainID:   checkUpstream.ChainID,
		Source:    checkUpstream.Source,
		Rpc:       checkUpstream.Rpc,
		CreatedAt: time.Now().UnixMilli(),
	}
	checkResult <- result
}

type Matcher struct {
	MatchType string `json:"matchType"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

type CheckRule struct {
	ID        int64  `json:"id"`
	ChainID   string `json:"chain_id"`
	Source    string `json:"source"`
	Rules     string `json:"rules"`
	CreatedAt int64  `json:"created_at"`
}

type pageInfo struct {
	Name      string `json:"name"`
	NextPage  int    `json:"next_page"`
	PageSize  int    `json:"page_size"`
	TotalPage int    `json:"total_page"`
}

func (pi *pageInfo) next(totalPage int) {
	pi.NextPage = pi.NextPage + 1
	if pi.NextPage > totalPage {
		pi.NextPage = 1
	}
	pi.TotalPage = totalPage
}

func (pi *pageInfo) reset() {
	pi.NextPage = 1
	pi.TotalPage = 0
}

/** main **/

func Check(db *sql.DB, chainIds []string) error {
	checker, err := newChecker(db, chainIds)
	if err != nil {
		return err
	}
	return checker.check()
}

func newChecker(db *sql.DB, chainIds []string) (*healthChecker, error) {
	return &healthChecker{
		queries:  pkg_db.New(db),
		db:       db,
		chainIds: chainIds,
	}, nil
}

type healthChecker struct {
	queries  *pkg_db.Queries
	db       *sql.DB
	chainIds []string
}

func (c *healthChecker) check() error {
	if err := c.newCheckInfo(); err != nil {
		return err
	}
	defer c.refreshCheckInfo()
	wg := &sync.WaitGroup{}

	upstreamMap, err := c.getUniqueUpstreams()
	if err != nil {
		return err
	}
	ruleGroups, err := c.getCheckRules()
	if err != nil {
		return err
	}

	var chainNum int
	for _, group := range ruleGroups {
		chainNum += len(group)
	}
	checkResult := make(chan *pkg_db.ReadyUpstream, chainNum)
	done := make(chan struct{}, 1)
	go c.collectCheckResult(done, checkResult)

	for sourceName, rules := range ruleGroups {
		for chainId, rule := range rules {
			upstreamEle, ok := upstreamMap[chainId]
			if !ok {
				continue
			}
			checkUpstream := &pkg_db.Upstream{
				ChainID:   chainId,
				Source:    sourceName,
				Rpc:       upstreamEle.Rpc,
				CreatedAt: time.Now().UnixMilli(),
			}
			wg.Add(1)
			go rule.Check(wg, checkResult, checkUpstream)
		}
	}

	wg.Wait()
	close(checkResult)
	<-done
	return nil
}

func (c *healthChecker) collectCheckResult(done chan struct{}, checkResult chan *pkg_db.ReadyUpstream) {
	defer close(done)
	var readyUpstreams []pkg_db.ReadyUpstream
	for readyUpstream := range checkResult {
		readyUpstreams = append(readyUpstreams, *readyUpstream)
	}
	upstreamWriter, err := upstream.NewUpstreamWriter(c.db)
	if err != nil {
		log.Println("get upstream writer error, err: ", err.Error())
		return
	}

	readyUpstreamGroup := types.NewArrayStream(readyUpstreams).GroupBy(func(u pkg_db.ReadyUpstream) string {
		return u.Source
	})
	if err = upstreamWriter.Refresh(readyUpstreamGroup); err != nil {
		log.Println("put upstream source error, err: ", err.Error())
		return
	}
	log.Println("check all upstream source done")
}

func (c *healthChecker) newCheckInfo() error {
	var keys []string
	for _, chainId := range c.chainIds {
		keys = append(keys, chainId+"_last_blocks")
	}
	kvCaches, err := c.queries.ListKvCacheInKeys(context.Background(), keys)
	if err != nil {
		return err
	}

	LAST_BLOCKS = make(map[string]*blockHeight)
	for _, kvCache := range kvCaches {
		height, err := strconv.ParseInt(kvCache.Value, 10, 64)
		if err != nil {
			return err
		}
		key := strings.TrimSuffix(kvCache.Key, "_last_blocks")
		LAST_BLOCKS[key] = &blockHeight{
			Value:  height,
			action: "none",
		}
	}
	return nil
}

func (c *healthChecker) refreshCheckInfo() error {
	ctx := context.Background()
	if len(LAST_BLOCKS) == 0 {
		return nil
	}

	for chainId, lastBlock := range LAST_BLOCKS {
		if lastBlock.action == "update" {
			if _, err := c.queries.UpdateKvCacheValue(ctx, pkg_db.UpdateKvCacheValueParams{
				Key:       chainId + "_last_blocks",
				Value:     strconv.FormatInt(lastBlock.Value, 10),
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				return err
			}
		}
		if lastBlock.action == "create" {
			if _, err := c.queries.CreateKvCache(ctx, pkg_db.CreateKvCacheParams{
				Key:       chainId + "_last_blocks",
				Value:     strconv.FormatInt(lastBlock.Value, 10),
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *healthChecker) getUniqueUpstreams() (map[string]pkg_db.Upstream, error) {
	upstreams, err := c.queries.ListUpstreamsInChainIdsAndSourceNotEq(context.Background(), pkg_db.ListUpstreamsInChainIdsAndSourceNotEqParams{
		ChainIds: c.chainIds,
		Source:   "custom/grpc",
	})
	if err != nil {
		return nil, err
	}

	upstreamMap := make(map[string]pkg_db.Upstream)
	for _, upstream := range upstreams {
		preUpstream, ok := upstreamMap[upstream.ChainID]
		if !ok {
			upstreamMap[upstream.ChainID] = upstream
		} else {
			preUpstream.Rpc = preUpstream.Rpc + "," + upstream.Rpc
			upstreamMap[upstream.ChainID] = preUpstream
		}
	}
	for _, upstream := range upstreamMap {
		upstream.Rpc = strings.Join(types.Rpc(upstream.Rpc).GetUrlsWithUnique(), ",")
	}
	return upstreamMap, nil
}

func (c *healthChecker) getCheckRules() (map[string]map[string]HealthCheckConditionList, error) {
	checkRules, err := c.queries.ListCheckRulesInChainIdsAndSourceNotEq(context.Background(), pkg_db.ListCheckRulesInChainIdsAndSourceNotEqParams{
		ChainIds: c.chainIds,
		Source:   "custom/grpc",
	})
	if err != nil {
		return nil, err
	}
	groupBySource := make(map[string]map[string]HealthCheckConditionList)
	for _, rule := range checkRules {
		sourceGroup, ok := groupBySource[rule.Source]
		if !ok {
			sourceGroup = make(map[string]HealthCheckConditionList)
			groupBySource[rule.Source] = sourceGroup
		}

		var conditions HealthCheckConditionList
		if err = json.Unmarshal([]byte(rule.Rules), &conditions); err != nil {
			return nil, err
		}
		sourceGroup[rule.ChainID] = conditions
	}
	return groupBySource, nil
}

type valueMatchChecker struct {
	types.JsonRpcCaller
}

func (c *valueMatchChecker) check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error) {
	condition.Matchers = types.NewArrayStream(condition.Matchers).Filter(func(m Matcher) bool {
		return m.MatchType == "=" || m.MatchType == "!="
	}).Collect()
	if len(condition.Matchers) == 0 {
		return nil, errors.New("invalid or empty matchers")
	}

	// cli := fetch.NewClient().HTTPClient(fetch.RedirectModeManual)
	cli := fetch.NewClient()

	// cli := http.DefaultClient
	// cli.Timeout = time.Second * 5

	ret := make(map[string]bool, len(urls))
	for _, url := range urls {
		var checkResult bool
		req, err := fetch.NewRequest(context.Background(), http.MethodPost, url, bytes.NewReader([]byte(condition.Payload)))
		if err != nil {
			return nil, err
		}
		values, err := FETCH_CACHE.match(req)
		if err != nil {
			return nil, err
		}
		if values == nil {
			values, err = c.Call(cli, req)
			if err != nil {
				log.Printf("check url %s error: %s\n", url, err.Error())
				ret[url] = checkResult
				continue
			}
			if values == nil || values["error"] != nil {
				ret[url] = checkResult
				continue
			}
			if err = FETCH_CACHE.put(req, values); err != nil {
				return nil, err
			}
		}

		for _, matcher := range condition.Matchers {
			var val string
			if val, err = types.TraverseField(values, strings.Split(matcher.Key, ".")); err != nil {
				return nil, err
			}
			checkResult = val == matcher.Value
			if matcher.MatchType == "!=" {
				checkResult = !checkResult
			}
			if !checkResult {
				break
			}
		}
		ret[url] = checkResult
	}
	return ret, nil
}

type blockHeightChecker struct {
	types.JsonRpcCaller
}

func (c *blockHeightChecker) check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error) {
	condition.Matchers = types.NewArrayStream(condition.Matchers).Filter(func(m Matcher) bool {
		return m.MatchType == "<" || m.MatchType == "<="
	}).Collect()
	if len(condition.Matchers) == 0 {
		return nil, errors.New("invalid or empty matchers")
	}

	// cli := fetch.NewClient().HTTPClient(fetch.RedirectModeFollow)
	cli := fetch.NewClient()
	// cli.Timeout = time.Second * 5

	matcher := condition.Matchers[0]
	ret := make(map[string]bool, len(urls))
	heightMap := make(map[string]int64, len(urls))
	heights := make([]int64, len(urls))
	for _, url := range urls {
		if condition.ignore(url) {
			ret[url] = true
			continue
		}

		req, err := fetch.NewRequest(context.Background(), http.MethodPost, url, bytes.NewReader([]byte(condition.Payload)))
		if err != nil {
			return nil, err
		}
		values, err := FETCH_CACHE.match(req)
		if err != nil {
			return nil, err
		}
		if values == nil {
			values, err = c.Call(cli, req)
			if err != nil {
				log.Printf("check url %s error: %s\n", url, err.Error())
				ret[url] = false
				continue
			}
			if values == nil || values["error"] != nil {
				ret[url] = false
				continue
			}
			if err = FETCH_CACHE.put(req, values); err != nil {
				return nil, err
			}
		}

		var val string
		if val, err = types.TraverseField(values, strings.Split(matcher.Key, ".")); err != nil {
			return nil, err
		}

		if val == "<no value>" {
			ret[url] = false
			continue
		}

		height, err := c.parseHeight(val)
		if err != nil {
			return nil, err
		}
		heightMap[url] = height
		heights = append(heights, height)
	}

	tolerance, err := c.parseHeight(matcher.Value)
	if err != nil {
		return nil, err
	}
	max := types.NewInt64Stream(heights).Max()
	lastBlock, ok := LAST_BLOCKS[chainId]
	if !ok {
		lastBlock = &blockHeight{
			action: "create",
			Value:  max,
		}
		LAST_BLOCKS[chainId] = lastBlock
	} else {
		if lastBlock.Value > max {
			max = lastBlock.Value
		} else {
			lastBlock.Value = max
			lastBlock.action = "update"
		}
	}

	for url, height := range heightMap {
		var checkResult bool
		if matcher.MatchType == "<" && max-height < tolerance {
			checkResult = true
		}
		if matcher.MatchType == "<=" && max-height <= tolerance {
			checkResult = true
		}
		ret[url] = checkResult
		if !checkResult {
			log.Printf("%d - %d %s %d, result: %t for %s\n", max, height, matcher.MatchType, tolerance, checkResult, url)
		}
	}
	return ret, nil
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
	types.JsonRpcCaller
}

func (c *simpleChecker) check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error) {
	cli := fetch.NewClient()
	ret := make(map[string]bool, len(urls))
	for _, url := range urls {
		req, err := fetch.NewRequest(context.Background(), http.MethodPost, url, bytes.NewReader([]byte(condition.Payload)))
		if err != nil {
			return nil, err
		}
		values, err := FETCH_CACHE.match(req)
		if err != nil {
			return nil, err
		}
		if values == nil {
			values, err = c.Call(cli, req)
			if err != nil {
				log.Printf("check url %s error: %s\n", url, err.Error())
				ret[url] = false
				continue
			}
			if values == nil || values["error"] != nil {
				if values["error"] != nil {
					errorInfo, ok := values["error"].(map[string]interface{})
					if ok {
						log.Printf("check url %s error: %s\n", url, errorInfo["message"])
					} else {
						log.Printf("check url %s error: %v\n", url, values["error"])
					}
				}
				ret[url] = false
				continue
			}
			if err = FETCH_CACHE.put(req, values); err != nil {
				return nil, err
			}
		}
		ret[url] = true
	}
	return ret, nil
}

type manualChecker struct {
}

func (c *manualChecker) check(chainId string, urls []string, condition *HealthCheckCondition) (map[string]bool, error) {
	condition.Matchers = types.NewArrayStream(condition.Matchers).Filter(func(m Matcher) bool {
		return m.MatchType == "=" || m.MatchType == "!="
	}).Collect()
	if len(condition.Matchers) == 0 {
		return nil, errors.New("invalid or empty matchers")
	}

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
				log.Printf("url %s filter by manual\n", url)
				break
			}
		}
		ret[url] = checkResult
	}

	return ret, nil
}
