package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/pundix/chain-gateway/internal/checker"
)

const (
	ADMIN_PATH = "admin/v1"
)

type ChainGatewayClient struct {
	User     string
	Password string
	Cli      *http.Client
	RootPath string
}

func NewChainGatewayClient(user, password, rootPath string, cli *http.Client) (*ChainGatewayClient, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("rootPath is empty")
	}
	if user == "" {
		return nil, fmt.Errorf("user is empty")
	}
	if password == "" {
		return nil, fmt.Errorf("password is empty")
	}

	return &ChainGatewayClient{
		User:     user,
		Password: password,
		Cli:      cli,
		RootPath: rootPath,
	}, nil
}

type Upstream struct {
	ChainId  string   `json:"chain_id"`
	Source   string   `json:"source"`
	RPC      string   `json:"rpc"`
	Protocol Protocol `json:"protocol,omitempty"`
}

func (u Upstream) JsonStr() string {
	if u.RPC == "" {
		return "[]"
	}
	b, _ := json.Marshal(strings.Split(u.RPC, ","))
	return string(b)
}

func (cgc *ChainGatewayClient) PostReadyUpstream(u *Upstream) error {
	obj := "upstream/ready"
	urlStr := fmt.Sprintf("%s/%s/%s", cgc.RootPath, ADMIN_PATH, obj)

	var list []*Upstream
	list = append(list, u)
	reqBody, err := json.Marshal(list)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	return nil
}

type SecretKey struct {
	Group        string `json:"group"`
	Service      string `json:"service"`
	AccessKey    string `json:"access_key,omitempty"`
	SecretKey    string `json:"secret_key,omitempty"`
	AllowOrigins string `json:"allow_origins"`
	AllowIps     string `json:"allow_ips"`
	RouteRules   string `json:"route_rules"`
}

func (cgc *ChainGatewayClient) PostSecretKey(sk *SecretKey) error {
	urlStr := fmt.Sprintf("%s/%s/%s", cgc.RootPath, ADMIN_PATH, "secret")

	reqBody, err := json.Marshal(sk)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	return nil
}

type Config struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Module string `json:"module"`
}

func (cgc *ChainGatewayClient) PostConfig(c *Config) error {
	urlStr := fmt.Sprintf("%s/%s/%s", cgc.RootPath, ADMIN_PATH, "config")

	reqBody, err := json.Marshal(c)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	return nil
}

type Protocol string

const (
	PROTOCOL_JSONRPC Protocol = "jsonrpc"
	PROTOCOL_GRPC    Protocol = "grpc"
)

type CheckRule struct {
	ChainId  string                           `json:"chain_id"`
	Source   string                           `json:"source"`
	Rules    checker.HealthCheckConditionList `json:"rules"`
	Protocol Protocol                         `json:"protocol,omitempty"`
	Disabled bool                             `json:"disabled,omitempty"`
}
