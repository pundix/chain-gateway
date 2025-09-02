package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/olekukonko/tablewriter"
)

const (
	HOST      = ""
	ROOT_PATH = "admin/v1"
)

type ChainGatewayClient struct {
	User     string
	Password string
	Cli      *http.Client

	Ready   bool
	Source  string
	ChainId string

	ImportFile string

	AK string

	Group   string
	Service string
}

type Upstream struct {
	ChainId string `json:"chain_id"`
	RPC     string `json:"rpc"`
}

func (cgc *ChainGatewayClient) getUpstream(urlStr string) ([]Upstream, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	queryParams := url.Values{}
	queryParams.Add("source", cgc.Source)
	if cgc.ChainId != "" {
		queryParams.Add("chainId", cgc.ChainId)
	}
	u.RawQuery = queryParams.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var upstreams []Upstream
	return upstreams, json.Unmarshal(body, &upstreams)
}

func (cgc *ChainGatewayClient) GetRule() error {
	obj := "rule"
	urlStr := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)
	u, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	queryParams := url.Values{}
	queryParams.Add("source", cgc.Source)
	if cgc.ChainId != "" {
		queryParams.Add("chainId", cgc.ChainId)
	}

	u.RawQuery = queryParams.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var CheckRules []CheckRule
	if err = json.Unmarshal(body, &CheckRules); err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ChainID", "Rules"})
	for _, r := range CheckRules {
		table.Append([]string{r.ChainId, r.Rules})
	}
	table.Render()
	return nil
}

type CheckRule struct {
	ChainId string `json:"chain_id"`
	Rules   string `json:"rules"`
}

func (cgc *ChainGatewayClient) GetUpstream() error {
	if cgc.Ready && cgc.ChainId == "" {
		return fmt.Errorf("chainId is required when get ready upstream")
	}

	obj := "upstream"
	if cgc.Ready {
		obj = "upstream/ready"
	}

	urlStr := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)
	upstreams, err := cgc.getUpstream(urlStr)
	if err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	if cgc.Ready {
		table.SetHeader([]string{"ChainID", "Ready RPC"})
	} else {
		table.SetHeader([]string{"ChainID", "All RPC"})
	}
	for _, u := range upstreams {
		table.Append([]string{u.ChainId, strings.ReplaceAll(u.RPC, ",", " ")})
	}
	table.Render()
	return nil
}

func (cgc *ChainGatewayClient) ImportRules() error {
	file, err := os.Open(cgc.ImportFile)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filename := filepath.Base(cgc.ImportFile)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}
	writer.Close()

	obj := "rule/import"
	url := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}

	fmt.Printf("Uploaded check rules from file: %s\n", cgc.ImportFile)
	return nil
}

func (cgc *ChainGatewayClient) ImportUpstreams() error {
	file, err := os.Open(cgc.ImportFile)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filename := filepath.Base(cgc.ImportFile)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}
	source := strings.TrimSuffix(filename, filepath.Ext(filename))
	_ = writer.WriteField("source", source)
	writer.Close()

	obj := "upstream/import"
	url := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}

	fmt.Printf("Uploaded upstreams from file: %s\n", cgc.ImportFile)
	return nil
}

func (cgc *ChainGatewayClient) CheckUpstream() error {
	obj := "upstream/check"
	urlStr := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)
	u, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	queryParams := url.Values{}
	queryParams.Add("chainIds", cgc.ChainId)

	u.RawQuery = queryParams.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	fmt.Printf("Check upstream for %s success\n", cgc.ChainId)
	return nil
}

func (cgc *ChainGatewayClient) VrifySecret() error {
	obj := "secret/verify"
	url := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)

	body, err := json.Marshal(map[string]string{
		"access_key": cgc.AK,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var ret map[string]string
	if err = json.Unmarshal(bytes, &ret); err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Group", "Service"})
	table.Append([]string{ret["group"], ret["service"]})
	table.Render()
	return nil
}

func (cgc *ChainGatewayClient) GenSecret() error {
	obj := "secret/gen"
	url := fmt.Sprintf("https://%s/%s/%s", HOST, ROOT_PATH, obj)

	body, err := json.Marshal(map[string]string{
		"group":   cgc.Group,
		"service": cgc.Service,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cgc.User, cgc.Password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cgc.Cli.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var ret map[string]string
	if err = json.Unmarshal(bytes, &ret); err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Group", "Service", "AccessKey", "SecretKey"})
	table.Append([]string{ret["group"], ret["service"], ret["access_key"], ret["secret_key"]})
	table.Render()
	return nil
}
