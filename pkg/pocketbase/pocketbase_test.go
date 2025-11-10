package pocketbase

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientNew(t *testing.T) {
	cli := New("http://localhost:8090/")
	if cli.BaseURL != "http://localhost:8090" {
		t.Fatalf("expected BaseURL trimmed, got %q", cli.BaseURL)
	}
	if cli.HTTPClient == nil {
		t.Fatalf("expected HTTPClient not nil")
	}
	if cli.HTTPClient.Timeout != 30*time.Second {
		t.Fatalf("expected Timeout 30s, got %v", cli.HTTPClient.Timeout)
	}
}

func TestListRecords_Success(t *testing.T) {
	var capturedPath string
	var capturedQuery url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()

		resp := &ListResponse{
			Page:       2,
			PerPage:    100,
			TotalItems: 2,
			TotalPages: 1,
			Items: []map[string]any{
				{"id": "ae40239d2bc4477", "collectionName": "posts", "title": "test1"},
				{"id": "d08dfc4f4d84419", "collectionName": "posts", "title": "test2"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	opts := ListOptions{
		Page:      2,
		PerPage:   100,
		Sort:      "-created,id",
		Filter:    "(title~'abc' && created>'2022-01-01')",
		Expand:    "relField1,relField2.subRelField",
		Fields:    "*,expand.relField1.name",
		SkipTotal: true,
	}
	out, err := cli.ListRecords("posts", opts)
	if err != nil {
		t.Fatalf("ListRecords error: %v", err)
	}

	if capturedPath != "/api/collections/posts/records" {
		t.Fatalf("expected path /api/collections/posts/records, got %s", capturedPath)
	}

	if capturedQuery.Get("page") != "2" ||
		capturedQuery.Get("perPage") != "100" ||
		capturedQuery.Get("sort") != "-created,id" ||
		capturedQuery.Get("filter") != "(title~'abc' && created>'2022-01-01')" ||
		capturedQuery.Get("expand") != "relField1,relField2.subRelField" ||
		capturedQuery.Get("fields") != "*,expand.relField1.name" ||
		capturedQuery.Get("skipTotal") != "true" {
		t.Fatalf("unexpected query params: %v", capturedQuery)
	}

	if out.Page != 2 || out.PerPage != 100 || len(out.Items) != 2 {
		t.Fatalf("unexpected response content: %+v", out)
	}
}

func TestListRecords_Defaults(t *testing.T) {
	var capturedQuery url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		resp := &ListResponse{
			Page:       1,
			PerPage:    30,
			TotalItems: 0,
			TotalPages: 0,
			Items:      []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	out, err := cli.ListRecords("posts", ListOptions{})
	if err != nil {
		t.Fatalf("ListRecords error: %v", err)
	}
	if capturedQuery.Get("page") != "1" || capturedQuery.Get("perPage") != "30" {
		t.Fatalf("expected defaults page=1, perPage=30, got %v", capturedQuery)
	}
	if out.Page != 1 || out.PerPage != 30 || len(out.Items) != 0 {
		t.Fatalf("unexpected response content: %+v", out)
	}
}

func TestListRecords_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": 400,
			"message": "Something went wrong while processing your request. Invalid filter.",
			"data": {}
		}`))
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	_, err := cli.ListRecords("posts", ListOptions{
		Filter: "(title~'abc'",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 400 {
		t.Fatalf("expected status=400, got %d", apiErr.Status)
	}
	if apiErr.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestListRecords_NonJSONError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	_, err := cli.ListRecords("posts", ListOptions{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, ok := err.(*APIError); ok {
		t.Fatalf("expected non-APIError for non-JSON body, got %T", err)
	}
}

func TestGetFirstListItem_Success(t *testing.T) {
	var capturedQuery url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		resp := &ListResponse{
			Page:       1,
			PerPage:    1,
			TotalItems: 1,
			TotalPages: 1,
			Items: []map[string]any{
				{"id": "ae40239d2bc4477", "service": "svc", "group": "grp"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	item, err := cli.GetFirstListItem("secret_key", ListOptions{
		Filter: "access_key='abc'",
	})
	if err != nil {
		t.Fatalf("GetFirstListItem error: %v", err)
	}
	if capturedQuery.Get("page") != "1" || capturedQuery.Get("perPage") != "1" {
		t.Fatalf("expected GetFirstListItem force page=1, perPage=1, got %v", capturedQuery)
	}
	if item["id"] != "ae40239d2bc4477" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestGetFirstListItem_NoRows(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := &ListResponse{
			Page:       1,
			PerPage:    1,
			TotalItems: 0,
			TotalPages: 0,
			Items:      []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cli := New(ts.URL)
	cli.HTTPClient = ts.Client()

	_, err := cli.GetFirstListItem("secret_key", ListOptions{})
	if err == nil {
		t.Fatalf("expected sql.ErrNoRows, got nil")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
