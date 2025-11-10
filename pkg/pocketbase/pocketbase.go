package pocketbase

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	// apiToken   string
}

func New(baseURL string) *Client {
	// apiToken := os.Getenv("PB_API_TOKEN")
	// if apiToken == "" {
	// 	panic("PB_API_TOKEN is required")
	// }
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// apiToken: apiToken,
	}
}

type ListOptions struct {
	Page      int
	PerPage   int
	Sort      string
	Filter    string
	Expand    string
	Fields    string
	SkipTotal bool
}

type ListResponse struct {
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalItems int              `json:"totalItems"`
	TotalPages int              `json:"totalPages"`
	Items      []map[string]any `json:"items"`
}

type APIError struct {
	Status  int            `json:"status"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("api error (status=%d)", e.Status)
}

func (c *Client) GetFirstListItem(collectionIdOrName string, opts ListOptions) (map[string]any, error) {
	opts.Page = 1
	opts.PerPage = 1

	resp, err := c.ListRecords(collectionIdOrName, opts)
	if err != nil {
		return nil, err
	}
	if len(resp.Items) == 0 {
		return nil, sql.ErrNoRows
	}
	return resp.Items[0], nil
}

func (c *Client) ListRecords(collectionIdOrName string, opts ListOptions) (*ListResponse, error) {
	if collectionIdOrName == "" {
		return nil, fmt.Errorf("collectionIdOrName is required")
	}

	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PerPage <= 0 {
		opts.PerPage = 30
	}

	q := url.Values{}
	q.Set("page", fmt.Sprintf("%d", opts.Page))
	q.Set("perPage", fmt.Sprintf("%d", opts.PerPage))

	if opts.Sort != "" {
		q.Set("sort", opts.Sort)
	}
	if opts.Filter != "" {
		q.Set("filter", opts.Filter)
	}
	if opts.Expand != "" {
		q.Set("expand", opts.Expand)
	}
	if opts.Fields != "" {
		q.Set("fields", opts.Fields)
	}
	if opts.SkipTotal {
		q.Set("skipTotal", "true")
	}

	u := c.BaseURL + "/api/collections/" + url.PathEscape(collectionIdOrName) + "/records"
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if err := json.Unmarshal(body, &apiErr); err == nil && (apiErr.Message != "" || apiErr.Status != 0 || apiErr.Data != nil) {
			if apiErr.Status == 0 {
				apiErr.Status = resp.StatusCode
			}
			return nil, &apiErr
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out ListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
