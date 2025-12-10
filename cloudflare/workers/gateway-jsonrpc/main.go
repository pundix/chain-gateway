package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/syumai/workers"
	"github.com/syumai/workers/cloudflare/fetch"

	pkg_db "github.com/pundix/chain-gateway/cloudflare/pkg/db"
	"github.com/pundix/chain-gateway/cloudflare/pkg/types"
	_ "github.com/syumai/workers/cloudflare/d1"
)

func main() {
	db, err := sql.Open("d1", "DB")
	if err != nil {
		log.Printf("error opening DB: %s\n", err.Error())
		return
	}
	defer db.Close()

	handler := &proxyHandler{
		queries: pkg_db.New(db),
	}
	http.HandleFunc("/v2/", handler.handleV2)
	http.HandleFunc("/v1/", handler.handleV1)
	workers.Serve(nil)
}

type proxyHandler struct {
	queries *pkg_db.Queries
}

func (h *proxyHandler) handleV1(w http.ResponseWriter, req *http.Request) {
	reqParams, err := h.parseV1PathParameters(req.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.handle(reqParams, w, req)
}

func (h *proxyHandler) handleV2(w http.ResponseWriter, req *http.Request) {
	reqParams, err := h.parseV2PathParameters(req.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.handle(reqParams, w, req)
}

func (h *proxyHandler) handle(reqParams *requestParams, w http.ResponseWriter, req *http.Request) {
	reqParams.startTime = time.Now()

	// auth
	sk, err := h.queries.GetSecretKeyByAccessKey(req.Context(), reqParams.accessKey)
	if err != nil {
		http.Error(w, "invalid access key", http.StatusUnauthorized)
		return
	}
	var accessControlAllowOrigin string
	if sk.AllowOrigins == "" {
		// sk.AllowOrigins = ".*"
		accessControlAllowOrigin = "*"
	} else {
		regex, err := regexp.Compile(sk.AllowOrigins)
		if err != nil {
			http.Error(w, "invalid allow origins", http.StatusInternalServerError)
			return
		}
		if regex.MatchString(req.Header.Get("Origin")) {
			accessControlAllowOrigin = req.Header.Get("Origin")
		} else {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
	}

	if req.Method == http.MethodOptions {
		// support cors
		w = h.handleCors(w, accessControlAllowOrigin)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method != http.MethodPost && req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", accessControlAllowOrigin)

	query := req.URL.Query()
	if reqParams.chainId == "" {
		reqParams.chainId = query.Get("chainId")
	}
	if reqParams.chainId == "" {
		http.Error(w, "chainId is required", http.StatusBadRequest)
		return
	}
	reqParams.source = query.Get("source")

	if req.Method == http.MethodGet {
		h.handleGetMethod(reqParams, w, req)
	} else {
		service := req.URL.Query().Get("service")
		if service == "" {
			service = sk.Service
		}
		requestTraceBuilder := newRequestTraceBuilder(service, sk.Group)
		defer req.Body.Close()
		reqBodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if err = requestTraceBuilder.withRequest(reqBodyBytes, req.Header); err != nil {
			http.Error(w, "failed to parse request body", http.StatusBadRequest)
			return
		}
		reqParams.rpcMethod = requestTraceBuilder.rt.Method
		reqParams.httpMethod = req.Method
		reqParams.body = reqBodyBytes
		reqParams.headers = req.Header.Clone()

		err = h.applyRouteRules(req.Context(), reqParams, sk)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.handlePostMethod(req.Context(), requestTraceBuilder, reqParams, w)
	}
}

type methodRouteRule struct {
	Source   string `json:"source"`
	ChainIds string `json:"chainIds"`
}

func (r methodRouteRule) match(chainId string) bool {
	chainIds := strings.Split(r.ChainIds, ",")
	for _, ID := range chainIds {
		if chainId == ID {
			return true
		}
	}
	return false
}

func (h *proxyHandler) applyRouteRules(ctx context.Context, reqParams *requestParams, sk pkg_db.SecretKey) error {
	config, err := h.queries.GetConfigByKey(ctx, pkg_db.GetConfigByKeyParams{
		Key:    "route_rules",
		Module: "upstream",
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var routeRules map[string]methodRouteRule
	// global route rule
	if config.Value != "" {
		if err = json.Unmarshal([]byte(config.Value), &routeRules); err != nil {
			return err
		}
	}

	if sk.RouteRules != "" {
		var skRouteRules map[string]methodRouteRule
		if err = json.Unmarshal([]byte(sk.RouteRules), &skRouteRules); err != nil {
			return err
		}
		if routeRules == nil {
			routeRules = skRouteRules
		} else {
			// override global route rule
			for k, routeRule := range skRouteRules {
				routeRules[k] = routeRule
			}
		}
	}

	// method route rule
	if rule, ok := routeRules[reqParams.rpcMethod]; ok && rule.match(reqParams.chainId) {
		reqParams.source = rule.Source
	}
	return nil
}

func (h *proxyHandler) handlePostMethod(ctx context.Context, requestTraceBuilder *requestTraceBuilder, reqParams *requestParams, w http.ResponseWriter) {
	requestTraceBuilder.withChainIdAndSource(reqParams.chainId, reqParams.source)
	endpointMap, err := h.getChainEndpoins(ctx, reqParams)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		requestTraceBuilder.withError(http.StatusInternalServerError, err.Error())
		requestTraceBuilder.Build().Println()
		return
	}
	if len(endpointMap) == 0 {
		http.Error(w, "chainId not support, no available nodes", http.StatusBadRequest)
		requestTraceBuilder.withError(http.StatusBadRequest, "chainId not support, no available nodes")
		requestTraceBuilder.Build().Println()
		return
	}
	if reqParams.source != "" {
		source := reqParams.source
		if !reqParams.isPaidMode() {
			source = "free"
		}
		if arr, ok := endpointMap[source]; !ok || len(arr) == 0 {
			http.Error(w, "source not support, no available nodes", http.StatusBadRequest)
			requestTraceBuilder.withError(http.StatusBadRequest, "source not support, no available nodes")
			requestTraceBuilder.Build().Println()
			return
		}
	}

	var targetUrls []string
	if reqParams.isTxMethod() {
		requestTraceBuilder.withMode("paid_tx")
		var arr []string
		var ok bool
		if reqParams.isMevMode() {
			requestTraceBuilder.withMode("mev_tx")
			arr, ok = endpointMap["free"]
			if !ok || len(arr) == 0 {
				http.Error(w, "chainId or source not support, no available nodes", http.StatusBadRequest)
				requestTraceBuilder.withError(http.StatusBadRequest, "chainId or source not support, no available nodes")
				requestTraceBuilder.Build().Println()
				return
			}
		} else {
			arr, ok = endpointMap["paid"]
			if !ok || len(arr) == 0 {
				// http.Error(w, "paid source not support, no available nodes", http.StatusBadRequest)
				// requestTraceBuilder.withError(http.StatusBadRequest, "paid source not support, no available nodes")
				// requestTraceBuilder.Build().Println()
				// return
				requestTraceBuilder.withMode("free_tx")
				arr, ok = endpointMap["free"]
				if !ok || len(arr) == 0 {
					http.Error(w, "chainId or source not support, no available nodes", http.StatusBadRequest)
					requestTraceBuilder.withError(http.StatusBadRequest, "chainId or source not support, no available nodes")
					requestTraceBuilder.Build().Println()
					return
				}
			}
		}
		i := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(arr))
		targetUrls = append(targetUrls, arr[i])
		// requestTraceBuilder.withUpstreamNode(targetUrl)
	} else if reqParams.isPaidMode() {
		requestTraceBuilder.withMode("paid_query")
		arr := endpointMap["paid"]
		random := rand.New(rand.NewSource(time.Now().UnixNano()))
		random.Shuffle(len(arr), func(i, j int) {
			arr[i], arr[j] = arr[j], arr[i]
		})
		if len(arr) > 3 {
			targetUrls = arr[:3]
		} else {
			targetUrls = arr
		}
	} else {
		requestTraceBuilder.withMode("free_query")
		arr := endpointMap["free"]
		random := rand.New(rand.NewSource(time.Now().UnixNano()))
		random.Shuffle(len(arr), func(i, j int) {
			arr[i], arr[j] = arr[j], arr[i]
		})
		if len(arr) > 3 {
			targetUrls = arr[:3]
		} else {
			targetUrls = arr
		}
		if !reqParams.isMevMode() {
			if arr, ok := endpointMap["paid"]; ok && len(arr) > 0 {
				i := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(arr))
				targetUrls = append(targetUrls, arr[i])
			}
		}
	}

	var respBodyBytes []byte
	cli := fetch.NewClient()
	resp, err := callFuncWithRetry(len(targetUrls), func(i int) (*http.Response, error) {
		if i != 0 {
			requestTraceBuilder.incrementRetries()
		}
		targetUrl := targetUrls[i]
		requestTraceBuilder.withUpstreamNode(targetUrl)

		r, err := fetch.NewRequest(ctx, reqParams.httpMethod, targetUrl, bytes.NewReader(reqParams.body))
		if err != nil {
			return nil, err
		}
		r.Header = reqParams.headers

		resp, err := cli.Do(r, nil)
		if resp != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			return nil, err
		}

		// resp body bytes > 5MB
		if resp.ContentLength > 5*1024*1024 {
			requestTraceBuilder.withLargeResponse(time.Since(reqParams.startTime).Milliseconds())
			return resp, nil
		}

		respBodyBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if err = requestTraceBuilder.withResponse(resp.StatusCode, respBodyBytes, time.Since(reqParams.startTime).Milliseconds()); err != nil {
			return nil, err
		}

		if requestTraceBuilder.rt.Status == "200" ||
			requestTraceBuilder.rt.Status == "3" ||
			requestTraceBuilder.rt.Status == "200&200" {
			return resp, nil
		}
		return resp, &RetryableError{
			Code:    resp.StatusCode,
			Message: resp.Status,
		}
	})
	if err != nil {
		if _, ok := err.(*RetryableError); !ok {
			requestTraceBuilder.withError(http.StatusInternalServerError, err.Error())
			requestTraceBuilder.Build().Println()
			return
		}
	}
	requestTraceBuilder.withVersion("v2.1")

	if resp == nil {
		w.WriteHeader(http.StatusInternalServerError)
		requestTraceBuilder.withError(http.StatusInternalServerError, "Response is nil")
		requestTraceBuilder.Build().Println()
		return
	}

	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Set(k, v)
		}
	}
	w.Header().Set("X-CGV2-Version", "v2.1")

	rt := requestTraceBuilder.Build()
	if rt.Status == "207" {
		// large response, copy body to writer
		if resp.Body != nil {
			// large response, copy body to writer
			io.Copy(w, resp.Body)
			resp.Body.Close()
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			requestTraceBuilder.withError(http.StatusInternalServerError, "Response body is nil")
			requestTraceBuilder.Build().Println()
		}
	} else {
		if len(respBodyBytes) == 0 {
			w.WriteHeader(resp.StatusCode)
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(int64(len(respBodyBytes)), 10))
			w.Write(respBodyBytes)
		}
	}
	rt.Println()
}

func (h *proxyHandler) handleCors(w http.ResponseWriter, origin string) http.ResponseWriter {
	w.Header().Add("Access-Control-Allow-Origin", origin)
	w.Header().Add("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Add("Access-Control-Max-Age", "86400")
	w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
	return w
}

func (h *proxyHandler) handleGetMethod(reqParams *requestParams, w http.ResponseWriter, req *http.Request) {
	endpointMap, err := h.getChainEndpoins(req.Context(), reqParams)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(endpointMap) == 0 {
		http.Error(w, "chainId not support, no available nodes", http.StatusBadRequest)
		return
	}
	if reqParams.source != "" {
		source := reqParams.source
		if !reqParams.isPaidMode() {
			source = "free"
		}
		if _, ok := endpointMap[source]; !ok {
			http.Error(w, "source not support, no available nodes", http.StatusBadRequest)
			return
		}
		endpointMap = map[string][]string{
			reqParams.source: endpointMap[source],
		}
	}

	var ret []string
	// desensitize
	for _, endpoints := range endpointMap {
		for i, endpoint := range endpoints {
			re := regexp.MustCompile(`.*/([a-z0-9]{32})`)
			match := re.FindStringSubmatch(endpoint)
			if len(match) == 2 {
				endpoints[i] = strings.Replace(endpoint, match[1], "REDACTED", 1)
			}

			re = regexp.MustCompile(`.*/([a-zA-Z-]{21})`)
			match = re.FindStringSubmatch(endpoint)
			if len(match) == 2 {
				endpoints[i] = strings.Replace(endpoint, match[1], "REDACTED", 1)
			}
		}
		ret = append(ret, endpoints...)
	}

	jsonBytes, err := json.Marshal(ret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

func (h *proxyHandler) getChainEndpoins(ctx context.Context, reqParams *requestParams) (map[string][]string, error) {
	upstreams, lastErr := callFuncWithRetry(3, func(_ int) ([]pkg_db.ReadyUpstream, error) {
		db, err := sql.Open("d1", "DB")
		if err != nil {
			log.Printf("error opening DB: %s\n", err.Error())
			return nil, err
		}
		defer db.Close()
		h.queries = pkg_db.New(db)
		return h.queries.ListReadyUpstreamsByChainId(ctx, reqParams.chainId)
	})
	if lastErr != nil {
		return nil, lastErr
	}
	if len(upstreams) == 0 {
		return map[string][]string{}, nil
	}

	upstreamMap := types.NewArrayStream(upstreams).ToMap(func(t pkg_db.ReadyUpstream) string {
		return t.Source
	})

	ret := map[string][]string{
		"free": {},
		"paid": {},
	}
	if upstream, ok := upstreamMap["paid"]; ok {
		if upstream.Rpc != "" {
			ret["paid"] = append(ret["paid"], types.Rpc(upstream.Rpc).GetUrlsWithUnique()...)
		}
	}
	if reqParams.source == "" {
		for _, upstream := range upstreamMap {
			if strings.Contains(upstream.Source, "paid") {
				continue
			}
			if upstream.Rpc != "" {
				ret["free"] = append(ret["free"], types.Rpc(upstream.Rpc).GetUrls()...)
			}
		}
		ret["free"] = types.NewStringStream(ret["free"]).Unique()
	} else {
		if reqParams.source != "paid" {
			if upstream, ok := upstreamMap[reqParams.source]; ok {
				if upstream.Rpc != "" {
					ret["free"] = append(ret["free"], types.Rpc(upstream.Rpc).GetUrlsWithUnique()...)
				}
			}
		}
	}

	return ret, nil
}

type requestParams struct {
	accessKey  string
	source     string
	chainId    string
	rpcMethod  string
	httpMethod string
	startTime  time.Time
	headers    http.Header
	body       []byte
}

func (rp *requestParams) isPaidMode() bool {
	return strings.Contains(rp.source, "paid")
}

func (rp *requestParams) isMevMode() bool {
	return strings.Contains(rp.source, "mev")
}

func (rp *requestParams) isTxMethod() bool {
	methods := []string{
		"eth_sign",
		"eth_signTransaction",
		"eth_sendTransaction",
		"eth_sendRawTransaction",
	}

	for _, method := range methods {
		if method == rp.rpcMethod {
			return true
		}
	}
	return false
}

func (h *proxyHandler) parseV1PathParameters(path string) (*requestParams, error) {
	re := regexp.MustCompile(`^/v1/([a-z0-9\-]+)/([a-z0-9]{32})$`)
	match := re.FindStringSubmatch(path)
	if len(match) == 0 {
		return nil, errors.New("invalid path")
	}
	reqParams := &requestParams{
		chainId:   match[1],
		accessKey: match[2],
	}
	return reqParams, nil
}

func (h *proxyHandler) parseV2PathParameters(path string) (*requestParams, error) {
	re := regexp.MustCompile(`^/v2/([a-z0-9]{32})$`)
	match := re.FindStringSubmatch(path)
	if len(match) == 0 {
		return nil, errors.New("invalid path")
	}
	reqParams := &requestParams{
		accessKey: match[1],
	}
	return reqParams, nil
}

func callFuncWithRetry[T any](callTimes int, fn func(int) (T, error)) (T, error) {
	var lastErr error
	var ret T
	for i := 0; i < callTimes; i++ {
		ret, lastErr = fn(i)
		if lastErr == nil {
			break
		}
	}
	return ret, lastErr
}

type requestTrace struct {
	Protocol  string      `json:"protocol"`
	ID        interface{} `json:"id"`
	Method    string      `json:"method"`
	ChainId   string      `json:"chainId"`
	Source    string      `json:"source"`
	Url       string      `json:"url"`
	Latency   int64       `json:"latency"`
	Group     string      `json:"group"`
	Service   string      `json:"service"`
	Status    string      `json:"status"`
	Message   string      `json:"message"`
	VisitorIp string      `json:"visitorIp"`
	Origin    string      `json:"origin"`
	Version   string      `json:"version"`
	Retries   int         `json:"retries"`
	Mode      string      `json:"mode"`
}

func (rt *requestTrace) Println() {
	bytes, _ := json.Marshal(rt)
	fmt.Println(string(bytes))
}

type requestTraceBuilder struct {
	rt *requestTrace
}

type JsonRPCResponse struct {
	// Jsonrpc string `json:"jsonrpc"`
	// ID      interface{}   `json:"id"`
	// Method string `json:"method"`
	// Params  interface{}   `json:"params"`
	Error *JsonRPCError `json:"error"`
}

type JsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type JsonRPCRequest struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	// Params interface{} `json:"params"`
}

func newRequestTraceBuilder(service, group string) *requestTraceBuilder {
	return &requestTraceBuilder{
		rt: &requestTrace{
			Protocol: "jsonrpc",
			Service:  service,
			Group:    group,
		},
	}
}

func (b *requestTraceBuilder) withError(code int, message string) {
	b.rt.Status = strconv.Itoa(code)
	b.rt.Message = message
}

func (b *requestTraceBuilder) withVersion(version string) {
	b.rt.Version = version
}

func (b *requestTraceBuilder) withMode(mode string) {
	b.rt.Mode = mode
}

func (b *requestTraceBuilder) withResponse(statusCode int, bodyBytes []byte, latency int64) error {
	b.rt.Latency = latency
	if statusCode != http.StatusOK {
		b.rt.Status = strconv.Itoa(statusCode)
		b.rt.Message = string(bodyBytes)
		return nil
	}

	var jsonResponse JsonRPCResponse
	var jsonResponses []JsonRPCResponse
	if err := json.Unmarshal(bodyBytes, &jsonResponse); err != nil {
		if err := json.Unmarshal(bodyBytes, &jsonResponses); err != nil {
			// unexpected end of JSON input
			b.rt.Status = strconv.Itoa(statusCode)
			b.rt.Message = string(bodyBytes)
			return nil
		}
		if len(jsonResponses) == 0 {
			return errors.New("empty response")
		}
	}
	if len(jsonResponses) == 0 {
		jsonResponses = append(jsonResponses, jsonResponse)
	}

	var status []string
	var message []string
	for _, jr := range jsonResponses {
		if jr.Error != nil {
			status = append(status, strconv.Itoa(jr.Error.Code))
			message = append(message, jr.Error.Message)
		} else {
			status = append(status, "200")
			message = append(message, "OK")
		}
	}
	b.rt.Status = strings.Join(status, "&")
	b.rt.Message = strings.Join(message, "&")
	return nil
}

func (b *requestTraceBuilder) incrementRetries() {
	b.rt.Retries = b.rt.Retries + 1
}

func (b *requestTraceBuilder) withUpstreamNode(url string) {
	b.rt.Url = url
}

func (b *requestTraceBuilder) withChainIdAndSource(chainId, source string) {
	b.rt.ChainId = chainId
	b.rt.Source = source
}

func (b *requestTraceBuilder) withRequest(bodyBytes []byte, header http.Header) error {
	var jsonRequest JsonRPCRequest
	var jsonRequestList []JsonRPCRequest
	if err := json.Unmarshal(bodyBytes, &jsonRequest); err != nil {
		if err = json.Unmarshal(bodyBytes, &jsonRequestList); err != nil {
			return err
		}
		if len(jsonRequestList) == 0 {
			return errors.New("empty request")
		}
	}
	if len(jsonRequestList) == 0 {
		jsonRequestList = append(jsonRequestList, jsonRequest)
	}

	var methods []string
	var ids []string

	for _, jr := range jsonRequestList {
		methods = append(methods, jr.Method)
		switch v := jr.ID.(type) {
		case string:
			ids = append(ids, v)
		case float64:
			ids = append(ids, fmt.Sprintf("%.0f", v))
		case int:
			ids = append(ids, strconv.Itoa(v))
		case nil:
			ids = append(ids, "null")
		default:
			ids = append(ids, fmt.Sprintf("%v", v))
		}
	}
	b.rt.ID = strings.Join(ids, "&")
	b.rt.Method = strings.Join(methods, "&")
	b.rt.VisitorIp = header.Get("CF-Connecting-IP")
	b.rt.Origin = header.Get("origin")
	return nil
}

func (b *requestTraceBuilder) withLargeResponse(latency int64) {
	b.rt.Status = strconv.Itoa(http.StatusMultiStatus)
	b.rt.Message = "Response entity too large"
	b.rt.Latency = latency
}

func (b *requestTraceBuilder) Build() *requestTrace {
	return b.rt
}

type RetryableError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RetryableError) Error() string {
	return e.Message
}
