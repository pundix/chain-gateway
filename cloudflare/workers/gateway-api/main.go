package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	pkg_db "github.com/pundix/chain-gateway/cloudflare/pkg/db"
	"github.com/syumai/workers"
	_ "github.com/syumai/workers/cloudflare/d1"
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
		// db:      db,
	}

	http.HandleFunc("/admin/v1/secret", handler.postSecretKey)
	http.HandleFunc("/admin/v1/upstream/ready", handler.postReadyUpstream)
	http.HandleFunc("/admin/v1/config", handler.postConfig)
	workers.Serve(nil) // use http.DefaultServeMux
}

type adminHandler struct {
	queries *pkg_db.Queries
	db      *sql.DB
}

func (h *adminHandler) postSecretKey(w http.ResponseWriter, req *http.Request) {
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

	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var secretKey pkg_db.SecretKey
	if err = json.Unmarshal(jsonBytes, &secretKey); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if secretKey.AccessKey == "" || secretKey.Group == "" || secretKey.Service == "" {
		http.Error(w, "invalid secretKey", http.StatusBadRequest)
		return
	}

	mode := "update"
	_, err = h.queries.GetSecretKeyByAccessKey(context.Background(), secretKey.AccessKey)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mode = "create"
	}

	if mode == "update" {
		if _, err = h.queries.UpdateSecretKey(context.Background(), pkg_db.UpdateSecretKeyParams{
			AccessKey:    secretKey.AccessKey,
			Service:      secretKey.Service,
			Group:        secretKey.Group,
			AllowOrigins: secretKey.AllowOrigins,
			AllowIps:     secretKey.AllowIps,
			RouteRules:   secretKey.RouteRules,
			Updated:      time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if _, err = h.queries.CreateSecretKey(context.Background(), pkg_db.CreateSecretKeyParams{
			AccessKey:    secretKey.AccessKey,
			SecretKey:    secretKey.SecretKey,
			Service:      secretKey.Service,
			Group:        secretKey.Group,
			AllowOrigins: secretKey.AllowOrigins,
			AllowIps:     secretKey.AllowIps,
			RouteRules:   secretKey.RouteRules,
			Created:      time.Now().UnixMilli(),
			Updated:      time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Write([]byte("OK"))
}

func (h *adminHandler) postReadyUpstream(w http.ResponseWriter, req *http.Request) {
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

	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var readyUpstream pkg_db.ReadyUpstream
	if err = json.Unmarshal(jsonBytes, &readyUpstream); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if readyUpstream.ChainID == "" || readyUpstream.Source == "" || readyUpstream.Rpc == "" {
		http.Error(w, "invalid readyUpstream", http.StatusBadRequest)
		return
	}

	mode := "update"
	dbReadyUpstream, err := h.queries.GetReadyUpstreamByChainIdSource(req.Context(), pkg_db.GetReadyUpstreamByChainIdSourceParams{
		ChainID: readyUpstream.ChainID,
		Source:  readyUpstream.Source,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mode = "create"
	}
	if mode == "update" {
		if dbReadyUpstream.Rpc == readyUpstream.Rpc {
			w.Write([]byte("OK"))
			return
		}
		if _, err = h.queries.UpdateReadyUpstreamRpc(req.Context(), pkg_db.UpdateReadyUpstreamRpcParams{
			ChainID: readyUpstream.ChainID,
			Source:  readyUpstream.Source,
			Rpc:     readyUpstream.Rpc,
			Updated: time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if _, err = h.queries.CreateReadyUpstream(req.Context(), pkg_db.CreateReadyUpstreamParams{
			ChainID: readyUpstream.ChainID,
			Source:  readyUpstream.Source,
			Rpc:     readyUpstream.Rpc,
			Created: time.Now().UnixMilli(),
			Updated: time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Write([]byte("OK"))
}

func (h *adminHandler) verifyBasicAuth(r *http.Request) (bool, error) {
	user, pass, ok := r.BasicAuth()
	config, err := h.queries.GetConfigByKey(r.Context(), pkg_db.GetConfigByKeyParams{
		Key:    "basic_auth",
		Module: "admin",
	})
	if err != nil {
		return false, err
	}
	auth := strings.Split(config.Value, " ")
	if !ok || user != auth[0] || pass != auth[1] {
		return false, nil
	}
	return true, nil
}

func (h *adminHandler) postConfig(w http.ResponseWriter, req *http.Request) {
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

	jsonBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var config pkg_db.Config
	if err = json.Unmarshal(jsonBytes, &config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if config.Key == "" || config.Module == "" || config.Value == "" {
		http.Error(w, "key, module or value is empty", http.StatusBadRequest)
		return
	}

	mode := "update"
	dbConfig, err := h.queries.GetConfigByKey(req.Context(), pkg_db.GetConfigByKeyParams{
		Key:    config.Key,
		Module: config.Module,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mode = "create"
	}

	if mode == "update" {
		if dbConfig.Value == config.Value {
			w.Write([]byte("OK"))
			return
		}
		if _, err = h.queries.UpdateConfigValue(req.Context(), pkg_db.UpdateConfigValueParams{
			Key:     config.Key,
			Module:  config.Module,
			Value:   config.Value,
			Updated: time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if _, err = h.queries.CreateConfig(req.Context(), pkg_db.CreateConfigParams{
			Key:     config.Key,
			Module:  config.Module,
			Value:   config.Value,
			Created: time.Now().UnixMilli(),
			Updated: time.Now().UnixMilli(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Write([]byte("OK"))
}
