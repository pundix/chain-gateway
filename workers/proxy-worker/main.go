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

	pkg_db "github.com/pundix/chain-gateway/pkg/db"
	"github.com/pundix/chain-gateway/pkg/types"
	"github.com/syumai/workers"
	_ "github.com/syumai/workers/cloudflare/d1"
	"github.com/syumai/workers/cloudflare/fetch"
)

var methodRouteRules = map[string]methodRouteRule{
	"eth_sign": methodRouteRule{
		Source:   "paid",
		ChainIds: "1,56,97",
	},
	"eth_signTransaction": methodRouteRule{
		Source:   "paid",
		ChainIds: "1,56,97",
	},
	"eth_sendTransaction": methodRouteRule{
		Source:   "paid",
		ChainIds: "1,56,97",
	},
	"eth_sendRawTransaction": methodRouteRule{
		Source:   "paid",
		ChainIds: "1,56,97",
	},
}

type methodRouteRule struct {
	Source   string
	ChainIds string
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
	http.HandleFunc("/v1/logs", handler.collectLogs)
	workers.Serve(nil)
}

type proxyHandler struct {
	types.WorkersHandler
	queries *pkg_db.Queries
}

func (h *proxyHandler) handleV1(w http.ResponseWriter, req *http.Request) {
	pathParams, err := parseV1PathParameters(req.URL.Path)
	if err != nil {
		h.Error(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	h.handle(pathParams, w, req)
}

func (h *proxyHandler) handleV2(w http.ResponseWriter, req *http.Request) {
	pathParams, err := parseV2PathParameters(req.URL.Path)
	if err != nil {
		h.Error(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	h.handle(pathParams, w, req)
}

func (h *proxyHandler) handleCors(w http.ResponseWriter, origin string) http.ResponseWriter {
	w.Header().Add("Access-Control-Allow-Origin", origin)
	w.Header().Add("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Add("Access-Control-Max-Age", "86400")
	w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
	return w
}

func (h *proxyHandler) handleGetMethod(params map[string]string, w http.ResponseWriter, req *http.Request) {
	chainId := params["chainId"]
	source := params["source"]

	endpointMap, err := h.getChainEndpoins(source, chainId, req.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error(), err)
		return
	}
	if endpointMap == nil {
		h.Error(w, http.StatusBadRequest, "chainId or source not support, no available nodes", nil)
		return
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
		h.InternalServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

func (h *proxyHandler) handlePOSTMethod(startTime time.Time, requestTraceBuilder *requestTraceBuilder, params map[string]string, w http.ResponseWriter, req *http.Request) {
	chainId := params["chainId"]
	source := params["source"]

	defer req.Body.Close()
	reqBodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		h.Error(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	if err = requestTraceBuilder.withRequest(reqBodyBytes, req.Header); err != nil {
		h.Error(w, http.StatusBadRequest, "failed to parse request body", err)
		return
	}

	if rule, ok := methodRouteRules[requestTraceBuilder.rt.Method]; ok {
		if rule.match(chainId) {
			source = rule.Source
		}
	}
	requestTraceBuilder.withChainIdAndSource(chainId, source)

	endpointMap, err := h.getChainEndpoins(source, chainId, req.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error(), err)
		return
	}
	if endpointMap == nil {
		requestTraceBuilder.withError(http.StatusBadRequest, "chainId or source not support, no available nodes")
		requestTraceBuilder.Build().Println()
		h.Error(w, http.StatusBadRequest, "no available nodes", nil)
		return
	}

	var endpoints []string
	for _, arr := range endpointMap {
		endpoints = append(endpoints, arr...)
	}

	i := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(endpoints))
	targetUrl := endpoints[i]
	requestTraceBuilder.withUpstreamNode(targetUrl)

	r, err := fetch.NewRequest(req.Context(), req.Method, targetUrl, bytes.NewReader(reqBodyBytes))
	if err != nil {
		h.InternalServerError(w, err)
		return
	}
	r.Header = req.Header.Clone()

	cli := fetch.NewClient()
	resp, err := cli.Do(r, nil)
	if err != nil {
		requestTraceBuilder.withError(http.StatusInternalServerError, err.Error())
		requestTraceBuilder.Build().Println()
		h.InternalServerError(w, err)
		return
	}

	defer resp.Body.Close()
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		h.InternalServerError(w, err)
		return
	}

	if len(respBodyBytes) > 1024*512 {
		requestTraceBuilder.withLargeResponse(time.Since(startTime).Milliseconds())
	} else {
		if err = requestTraceBuilder.withResponse(resp.StatusCode, respBodyBytes, time.Since(startTime).Milliseconds()); err != nil {
			h.InternalServerError(w, err)
			return
		}
	}
	requestTraceBuilder.withVersion("v2.0")
	requestTraceBuilder.Build().Println()

	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Set(k, v)
		}
	}

	if len(respBodyBytes) == 0 {
		w.WriteHeader(resp.StatusCode)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(respBodyBytes)), 10))
		w.Header().Set("X-CGV2-Version", "v2.0")
		w.Write(respBodyBytes)
	}
}

type RetryableError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RetryableError) Error() string {
	return e.Message
}

func (h *proxyHandler) handlePOSTMethodNew(startTime time.Time, requestTraceBuilder *requestTraceBuilder, params map[string]string, w http.ResponseWriter, req *http.Request) {
	// cloudflare.PassThroughOnException()
	chainId := params["chainId"]
	source := params["source"]

	defer req.Body.Close()
	reqBodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		h.Error(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	if err = requestTraceBuilder.withRequest(reqBodyBytes, req.Header); err != nil {
		h.Error(w, http.StatusBadRequest, "failed to parse request body", err)
		return
	}

	// paid mode
	paidMode := false
	// query method
	queryMethod := true
	if rule, ok := methodRouteRules[requestTraceBuilder.rt.Method]; ok {
		if rule.match(chainId) {
			source = rule.Source
			queryMethod = false
		}
	}
	if strings.HasPrefix(source, "paid") {
		paidMode = true
	}
	requestTraceBuilder.withChainIdAndSource(chainId, source)

	endpointMap, err := h.getChainEndpoins(source, chainId, req.Context())
	if err != nil {
		// lastErr = err
		w.WriteHeader(http.StatusInternalServerError)
		requestTraceBuilder.withError(http.StatusInternalServerError, err.Error())
		requestTraceBuilder.Build().Println()
		// h.Error(w, http.StatusInternalServerError, err.Error(), err)
		return
	}
	if endpointMap == nil {
		h.Error(w, http.StatusBadRequest, "chainId or source not support, no available nodes", nil)
		requestTraceBuilder.withError(http.StatusBadRequest, "chainId or source not support, no available nodes")
		requestTraceBuilder.Build().Println()
		return
	}

	var targetUrls []string
	if paidMode && !queryMethod {
		requestTraceBuilder.withMode("paid_not_query")
		i := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(endpointMap["paid"]))
		targetUrls = append(targetUrls, endpointMap["paid"][i])
		// requestTraceBuilder.withUpstreamNode(targetUrl)
	} else if paidMode && queryMethod {
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
		if len(endpointMap["paid"]) > 0 {
			i := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(endpointMap["paid"]))
			targetUrls = append(targetUrls, endpointMap["paid"][i])
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

		r, err := fetch.NewRequest(req.Context(), req.Method, targetUrl, bytes.NewReader(reqBodyBytes))
		if err != nil {
			return nil, err
		}
		r.Header = req.Header.Clone()

		resp, err := cli.Do(r, nil)
		if err != nil {
			return nil, err
		}

		// resp body bytes > 5MB
		if resp.ContentLength > 5*1024*1024 {
			requestTraceBuilder.withLargeResponse(time.Since(startTime).Milliseconds())
			return resp, nil
		}

		defer resp.Body.Close()
		respBodyBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if err = requestTraceBuilder.withResponse(resp.StatusCode, respBodyBytes, time.Since(startTime).Milliseconds()); err != nil {
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

func (h *proxyHandler) splitEndpoints(endpoints []string) map[string][]string {
	paidRegx := regexp.MustCompile(`.*infura.io.*|.*alchemy.com.*`)
	paidEndpoints := types.NewArrayStream(endpoints).Filter(func(t string) bool {
		return paidRegx.MatchString(t)
	}).Collect()
	freeEndpoints := types.NewArrayStream(endpoints).Filter(func(t string) bool {
		return !paidRegx.MatchString(t)
	}).Collect()
	return map[string][]string{
		"paid": paidEndpoints,
		"free": freeEndpoints,
	}
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

func (h *proxyHandler) getChainEndpoins(source, chainId string, ctx context.Context) (map[string][]string, error) {
	var lastErr error
	var readyUpStreams []pkg_db.ReadyUpstream
	if source != "" {
		readyUpStreams, lastErr = h.queries.ListReadyUpstreamsByChainIdSource(ctx, pkg_db.ListReadyUpstreamsByChainIdSourceParams{
			ChainID: chainId,
			Source:  source,
		})
	} else {
		readyUpStreams, lastErr = h.queries.ListReadyUpstreamsByChainIdSourceNotEq(ctx, pkg_db.ListReadyUpstreamsByChainIdSourceNotEqParams{
			ChainID: chainId,
			Source:  "custom/grpc",
		})
	}
	if lastErr != nil {
		readyUpStreams, lastErr = callFuncWithRetry(2, func(_ int) ([]pkg_db.ReadyUpstream, error) {
			db, err := sql.Open("d1", "DB")
			if err != nil {
				log.Printf("error opening DB: %s\n", err.Error())
				return nil, err
			}
			defer db.Close()
			h.queries = pkg_db.New(db)

			var readyUpStreams []pkg_db.ReadyUpstream
			if source != "" {
				readyUpStreams, err = h.queries.ListReadyUpstreamsByChainIdSource(ctx, pkg_db.ListReadyUpstreamsByChainIdSourceParams{
					ChainID: chainId,
					Source:  source,
				})
			} else {
				readyUpStreams, err = h.queries.ListReadyUpstreamsByChainIdSourceNotEq(ctx, pkg_db.ListReadyUpstreamsByChainIdSourceNotEqParams{
					ChainID: chainId,
					Source:  "custom/grpc",
				})
			}
			return readyUpStreams, err
		})
	}
	if lastErr != nil {
		// h.InternalServerError(w, err)
		return nil, lastErr
	}
	if len(readyUpStreams) == 0 {
		return nil, nil
		// h.Error(w, http.StatusBadRequest, "chainId or source not supported", nil)
		// return []string{}, sql.ErrNoRows
	}

	readyUpstream := readyUpStreams[0]
	for _, u := range readyUpStreams[1:] {
		if u.Rpc != "" {
			readyUpstream.Rpc = fmt.Sprintf("%s,%s", readyUpstream.Rpc, u.Rpc)
		}
	}

	// if readyUpstream.Rpc == "" {
	// 	requestTraceBuilder.withError(http.StatusServiceUnavailable, "no available nodes")
	// 	requestTraceBuilder.Build().Println()
	// 	h.Error(w, http.StatusServiceUnavailable, "no available nodes", nil)
	// 	return
	// }
	return h.splitEndpoints(types.Rpc(readyUpstream.Rpc).GetUrlsWithUnique()), nil
}

func (h *proxyHandler) handle(params map[string]string, w http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	// auth
	sk, err := h.queries.GetSecretKeyByAccessKey(req.Context(), params["access_key"])
	if err != nil {
		h.Error(w, http.StatusUnauthorized, "invalid access key", err)
		return
	}
	var accessControlAllowOrigin string
	if sk.AllowOrigins == "" {
		// sk.AllowOrigins = ".*"
		accessControlAllowOrigin = "*"
	} else {
		regex, err := regexp.Compile(sk.AllowOrigins)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, "invalid allow origins", err)
			return
		}
		if regex.MatchString(req.Header.Get("Origin")) {
			accessControlAllowOrigin = req.Header.Get("Origin")
		} else {
			h.Error(w, http.StatusUnauthorized, "origin not allowed", nil)
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
		h.Error(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", accessControlAllowOrigin)

	query := req.URL.Query()
	if _, ok := params["chainId"]; !ok {
		params["chainId"] = query.Get("chainId")
	}
	if params["chainId"] == "" {
		h.Error(w, http.StatusBadRequest, "chainId is required", nil)
		return
	}
	params["source"] = query.Get("source")

	if req.Method == http.MethodGet {
		h.handleGetMethod(params, w, req)
	} else {
		service := req.URL.Query().Get("service")
		if service == "" {
			service = sk.Service
		}
		requestTraceBuilder := newRequestTraceBuilder(service, sk.Group)

		// if rand.Float32() < 0.5 {
		// h.handlePOSTMethod(startTime, requestTraceBuilder, params, w, req)
		// } else {
		h.handlePOSTMethodNew(startTime, requestTraceBuilder, params, w, req)
		// }
	}
}

type JsonRPCRequest struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	// Params interface{} `json:"params"`
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

func newRequestTraceBuilder(service, group string) *requestTraceBuilder {
	return &requestTraceBuilder{
		rt: &requestTrace{
			Protocol: "jsonrpc",
			Service:  service,
			Group:    group,
		},
	}
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

func parseV1PathParameters(path string) (map[string]string, error) {
	re := regexp.MustCompile(`^/v1/([a-z0-9\-]+)/([a-z0-9]{32})$`)
	match := re.FindStringSubmatch(path)
	if len(match) == 0 {
		return nil, errors.New("invalid path")
	}
	ret := make(map[string]string, 2)
	ret["chainId"] = match[1]
	ret["access_key"] = match[2]
	return ret, nil
}

func parseV2PathParameters(path string) (map[string]string, error) {
	re := regexp.MustCompile(`^/v2/([a-z0-9]{32})$`)
	match := re.FindStringSubmatch(path)
	if len(match) == 0 {
		return nil, errors.New("invalid path")
	}
	ret := make(map[string]string, 1)
	ret["access_key"] = match[1]
	return ret, nil
}

type clientRequestTrace struct {
	Protocol string `json:"protocol"`
	Method   string `json:"method"`
	ChainId  string `json:"chainId"`
	Source   string `json:"source"`
	Url      string `json:"url"`
	Message  string `json:"message"`
	Success  bool   `json:"success"`
	Group    string `json:"group"`
	Service  string `json:"service"`
}

type clientRequestTraceList []*clientRequestTrace

func (h *proxyHandler) collectLogs(w http.ResponseWriter, req *http.Request) {
	accessKey := req.Header.Get("x-api-key")
	if accessKey == "" {
		h.Error(w, http.StatusUnauthorized, "invalid access key", nil)
		return
	}

	sk, err := h.queries.GetSecretKeyByAccessKey(req.Context(), accessKey)
	if err != nil {
		h.Error(w, http.StatusUnauthorized, "invalid access key", err)
		return
	}

	var requestTraceList clientRequestTraceList
	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		h.Error(w, http.StatusBadRequest, "failed to read request body", err)
		return
	}
	defer req.Body.Close()

	if err = json.Unmarshal(jsonBytes, &requestTraceList); err != nil {
		h.Error(w, http.StatusBadRequest, "failed to parse request body", err)
		return
	}

	for _, rt := range requestTraceList {
		rt.Group = sk.Group
		rt.Service = sk.Service
		bytes, _ := json.Marshal(rt)
		fmt.Println(string(bytes))
	}

	w.Write([]byte("OK"))
}
