package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pundix/chain-gateway/pkg/checker"
	pkg_db "github.com/pundix/chain-gateway/pkg/db"
	"github.com/pundix/chain-gateway/pkg/types"
	"github.com/pundix/chain-gateway/pkg/upstream"
	"github.com/syumai/workers"
	_ "github.com/syumai/workers/cloudflare/d1"
	"github.com/syumai/workers/cloudflare/queues"
)

func main() {
	db, err := sql.Open("d1", "DB")
	if err != nil {
		log.Printf("error opening DB: %s\n", err.Error())
		return
	}
	defer db.Close()

	handler := &adminHandler{
		queries: pkg_db.New(db),
		db:      db,
	}

	http.HandleFunc("/admin/v1/rule/import", handler.handleRulesImport)
	http.HandleFunc("/admin/v1/rule", handler.getRule)

	http.HandleFunc("/admin/v1/upstream/import", handler.handleUpstreamImport)
	http.HandleFunc("/admin/v1/upstream", handler.getUpstream)
	http.HandleFunc("/admin/v1/upstream/ready", handler.handleReadyUpstream)
	http.HandleFunc("/admin/v1/upstream/check", handler.handleUpstreamCheck)

	http.HandleFunc("/admin/v1/secret/import", handler.handleSecretImport)
	http.HandleFunc("/admin/v1/secret/gen", handler.handleSecretKeyGen)
	http.HandleFunc("/admin/v1/secret/verify", handler.verifySecret)

	workers.Serve(nil)
}

type adminHandler struct {
	queries *pkg_db.Queries
	db      *sql.DB
}

type importRule struct {
	Source string                                      `json:"source"`
	Rules  map[string]checker.HealthCheckConditionList `json:"rules"`
}

func (h *adminHandler) handleRulesImport(w http.ResponseWriter, req *http.Request) {
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if req.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var importRule importRule
	if err = json.Unmarshal(bytes, &importRule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbRules, err := h.queries.ListCheckRulesBySource(req.Context(), importRule.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	dbRuleMap := types.NewArrayStream(dbRules).ToMap(func(r pkg_db.CheckRule) string {
		return r.ChainID
	})

	for chainId, rule := range importRule.Rules {
		bytes, err := json.Marshal(rule)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		dbRule, ok := dbRuleMap[chainId]
		if !ok {
			if _, err = h.queries.CreateCheckRule(req.Context(), pkg_db.CreateCheckRuleParams{
				Source:    importRule.Source,
				ChainID:   chainId,
				Rules:     string(bytes),
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		} else {
			if dbRule.Rules != string(bytes) {
				if _, err = h.queries.UpdateCheckRule(req.Context(), pkg_db.UpdateCheckRuleParams{
					Source:    importRule.Source,
					ChainID:   chainId,
					Rules:     string(bytes),
					CreatedAt: time.Now().UnixMilli(),
				}); err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
	}

	fmt.Fprintf(w, "import check rules successfully: %s", header.Filename)
}

type CheckRule struct {
	ChainID string `json:"chain_id"`
	Rules   string `json:"rules"`
	Source  string `json:"source"`
}

func (h *adminHandler) getRule(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	source := req.URL.Query().Get("source")
	if source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	chainId := req.URL.Query().Get("chainId")
	var rules []pkg_db.CheckRule
	if chainId == "" {
		rules, err = h.queries.ListCheckRulesBySource(context.Background(), source)
	} else {
		rules, err = h.queries.ListCheckRulesByChainIdSource(context.Background(), pkg_db.ListCheckRulesByChainIdSourceParams{
			ChainID: chainId,
			Source:  source,
		})
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(rules) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	var ret []CheckRule
	for _, r := range rules {
		ret = append(ret, CheckRule{
			ChainID: r.ChainID,
			Rules:   r.Rules,
			Source:  r.Source,
		})
	}

	jsonBytes, err := json.Marshal(ret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

type SecretKeyGenReq struct {
	Group   string `json:"group"`
	Service string `json:"service"`
}

type SecretKeyGenResp struct {
	Group     string `json:"group"`
	Service   string `json:"service"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func (h *adminHandler) handleSecretKeyGen(w http.ResponseWriter, req *http.Request) {
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if req.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer req.Body.Close()

	bytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var secretKeyGenReq SecretKeyGenReq
	if err = json.Unmarshal(bytes, &secretKeyGenReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if secretKeyGenReq.Group == "" || secretKeyGenReq.Service == "" {
		http.Error(w, "group and service is required", http.StatusBadRequest)
		return
	}

	ak, err := h.genSecret(16)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sk, err := h.genSecret(32)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err = h.queries.CreateSecretKey(context.Background(), pkg_db.CreateSecretKeyParams{
		AccessKey: ak,
		SecretKey: sk,
		Group:     secretKeyGenReq.Group,
		Service:   secretKeyGenReq.Service,
		CreatedAt: time.Now().UnixMilli(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	resp := &SecretKeyGenResp{
		Group:     secretKeyGenReq.Group,
		Service:   secretKeyGenReq.Service,
		AccessKey: ak,
		SecretKey: sk,
	}
	bytes, err = json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(bytes)
}

type ImportSecret struct {
	Group     string `json:"group"`
	Service   string `json:"service"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func (h *adminHandler) handleSecretImport(w http.ResponseWriter, req *http.Request) {
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if req.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var importSecrets []*ImportSecret
	if err = json.Unmarshal(bytes, &importSecrets); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbSecrets, err := h.queries.ListSecretKeys(context.Background())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	dbSecretMap := types.NewArrayStream(dbSecrets).ToMap(func(sk pkg_db.SecretKey) string {
		return sk.AccessKey
	})

	for _, is := range importSecrets {
		if _, ok := dbSecretMap[is.AccessKey]; !ok {
			if _, err = h.queries.CreateSecretKey(context.Background(), pkg_db.CreateSecretKeyParams{
				AccessKey: is.AccessKey,
				SecretKey: is.SecretKey,
				Group:     is.Group,
				Service:   is.Service,
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}
	}

	fmt.Fprintf(w, "import secret keys successfully: %s", header.Filename)
}

func (h *adminHandler) genSecret(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

const (
	SOURCE_NAME_MANUAL = "manual"
)

func (h *adminHandler) verifyBasicAuth(r *http.Request) (bool, error) {
	user, pass, ok := r.BasicAuth()
	kv, err := h.queries.GetKvCacheByKey(context.Background(), "basic_auth")
	if err != nil {
		return false, err
	}
	auth := strings.Split(kv.Value, " ")
	if !ok || user != auth[0] || pass != auth[1] {
		return false, nil
	}
	return true, nil
}

func (h *adminHandler) handleUpstreamImport(w http.ResponseWriter, req *http.Request) {
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if req.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	source := req.FormValue("source")
	if source == "" {
		source = SOURCE_NAME_MANUAL
	}
	file, header, err := req.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var importUpstream map[string][]string
	if err = json.Unmarshal(bytes, &importUpstream); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dbUpstreams, err := h.queries.ListUpstreamsBySource(context.Background(), source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if len(dbUpstreams) == 0 {
		log.Printf("source not found: %v, add upstreams directly", source)
		for chainId, upstreams := range importUpstream {
			if _, err = h.queries.CreateUpstream(context.Background(), pkg_db.CreateUpstreamParams{
				Source:    source,
				ChainID:   chainId,
				Rpc:       strings.Join(upstreams, ","),
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}
		fmt.Fprintf(w, "import upstreams successfully: %s", header.Filename)
		return
	}
	upstreamMap := types.NewArrayStream(dbUpstreams).ToMap(func(u pkg_db.Upstream) string {
		return u.ChainID
	})

	for chainId, upstreams := range importUpstream {
		dbUpstream, ok := upstreamMap[chainId]
		if !ok {
			if _, err = h.queries.CreateUpstream(context.Background(), pkg_db.CreateUpstreamParams{
				Source:    source,
				ChainID:   chainId,
				Rpc:       strings.Join(upstreams, ","),
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		} else {
			if dbUpstream.Rpc != strings.Join(upstreams, ",") {
				if _, err = h.queries.UpdateUpstreamRpc(context.Background(), pkg_db.UpdateUpstreamRpcParams{
					Source:    source,
					ChainID:   chainId,
					Rpc:       strings.Join(upstreams, ","),
					CreatedAt: time.Now().UnixMilli(),
				}); err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
	}

	fmt.Fprintf(w, "import upstreams successfully: %s", header.Filename)
}

type Upstream struct {
	ChainID string `json:"chain_id"`
	Rpc     string `json:"rpc"`
}

func (h *adminHandler) getUpstream(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	source := req.URL.Query().Get("source")
	if source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	chainId := req.URL.Query().Get("chainId")
	var upstreams []pkg_db.Upstream
	if chainId != "" {
		upstreams, err = h.queries.ListUpstreamsByChainIdSource(context.Background(), pkg_db.ListUpstreamsByChainIdSourceParams{
			ChainID: chainId,
			Source:  source,
		})
	} else {
		upstreams, err = h.queries.ListUpstreamsBySource(context.Background(), source)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(upstreams) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	upstreamMap := make(map[string]pkg_db.Upstream)
	for _, upstream := range upstreams {
		preUpstream, ok := upstreamMap[upstream.ChainID]
		if !ok {
			upstreamMap[upstream.ChainID] = upstream
		} else {
			preUpstream.Rpc = preUpstream.Rpc + "," + upstream.Rpc
		}
	}

	var uniqueUpstreams []pkg_db.Upstream
	for _, upstream := range upstreamMap {
		rpc := strings.Split(upstream.Rpc, ",")
		upstream.Rpc = strings.Join(types.NewStringStream(rpc).Unique(), ",")
		uniqueUpstreams = append(uniqueUpstreams, upstream)
	}

	var ret []Upstream
	for _, u := range uniqueUpstreams {
		ret = append(ret, Upstream{
			ChainID: u.ChainID,
			Rpc:     u.Rpc,
		})
	}

	bytes, err := json.Marshal(ret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
	return
}

type ReadyUpstream struct {
	ChainID string `json:"chain_id"`
	Rpc     string `json:"rpc"`
}

func (h *adminHandler) handleReadyUpstream(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" && req.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if req.Method == "GET" {
		query := req.URL.Query()
		source := query.Get("source")
		if source == "" {
			http.Error(w, "source is required", http.StatusBadRequest)
			return
		}
		chainId := query.Get("chainId")
		if chainId == "" {
			http.Error(w, "chainId is required", http.StatusBadRequest)
			return
		}
		upstreams, err := h.queries.ListReadyUpstreamsByChainIdSource(context.Background(), pkg_db.ListReadyUpstreamsByChainIdSourceParams{
			ChainID: chainId,
			Source:  source,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(upstreams) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		var ret []ReadyUpstream
		for _, u := range upstreams {
			ret = append(ret, ReadyUpstream{
				ChainID: u.ChainID,
				Rpc:     u.Rpc,
			})
		}

		jsonBytes, err := json.Marshal(ret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
		return
	}

	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var readyUpstreams []pkg_db.ReadyUpstream
	if err = json.Unmarshal(jsonBytes, &readyUpstreams); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	readyUpstreamGroup := types.NewArrayStream(readyUpstreams).GroupBy(func(u pkg_db.ReadyUpstream) string {
		return u.Source
	})

	upstreamWriter, err := upstream.NewUpstreamWriter(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := upstreamWriter.Refresh(readyUpstreamGroup); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte("OK"))
}

const queueName = "QUEUE"

func (h *adminHandler) handleUpstreamCheck(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	chainIds := req.URL.Query().Get("chainIds")
	chains := strings.Split(chainIds, ",")

	q, err := queues.NewProducer(queueName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, chain := range chains {
		fmt.Printf("Send chain: %s\n", chain)
		if err := q.SendText(chain); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Write([]byte("OK"))
}

type SecretKeyVerifyReq struct {
	AccessKey string `json:"access_key"`
}

type SecretKeyVerifyResp struct {
	Service string `json:"service"`
	Group   string `json:"group"`
}

func (h *adminHandler) verifySecret(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok, err := h.verifyBasicAuth(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	defer req.Body.Close()
	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var secretKeyVerifyReq SecretKeyVerifyReq
	if err = json.Unmarshal(jsonBytes, &secretKeyVerifyReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sk, err := h.queries.GetSecretKeyByAccessKey(context.Background(), secretKeyVerifyReq.AccessKey)
	if err != nil {
		http.Error(w, "invalid accessKey", http.StatusUnauthorized)
		return
	}

	resp := &SecretKeyVerifyResp{
		Service: sk.Service,
		Group:   sk.Group,
	}
	jsonBytes, _ = json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}
