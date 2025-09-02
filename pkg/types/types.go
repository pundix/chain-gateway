package types

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/syumai/workers/cloudflare"
	"github.com/syumai/workers/cloudflare/fetch"
)

type TimedCache[T any] struct {
	value  T
	time   time.Time
	expire time.Duration
}

func NewTimedCache[T any](value T, expire time.Duration) *TimedCache[T] {
	return &TimedCache[T]{value: value, time: time.Now(), expire: expire}
}

func (c *TimedCache[T]) Get() (T, bool) {
	if time.Since(c.time) > c.expire {
		return c.value, false
	}
	return c.value, true
}

func (c *TimedCache[T]) Set(value T) {
	c.value = value
	c.time = time.Now()
}

func (c *TimedCache[T]) Expire() {
	c.time = time.Now().Add(-c.expire)
}

type Int64Stream struct {
	arr []int64
}

func NewInt64Stream(arr []int64) *Int64Stream {
	return &Int64Stream{
		arr: arr,
	}
}

func (s *Int64Stream) Max() int64 {
	var max int64
	for _, i := range s.arr {
		if i > max {
			max = i
		}
	}
	return max
}

type StringStream struct {
	arr []string
}

func NewStringStream(arr []string) *StringStream {
	return &StringStream{
		arr: arr,
	}
}

func (s *StringStream) Unique() []string {
	var result []string
	seen := make(map[string]bool)
	for _, str := range s.arr {
		if _, ok := seen[str]; !ok {
			result = append(result, str)
			seen[str] = true
		}
	}
	return result
}

type Stream[T any] struct {
	arr         []T
	transferArr []interface{}
}

func NewArrayStream[T any](arr []T) *Stream[T] {
	return &Stream[T]{
		arr: arr,
	}
}

func (s *Stream[T]) Map(f MapFunc[T]) *Stream[T] {
	for _, v := range s.arr {
		s.transferArr = append(s.transferArr, f(v))
	}
	return s
}

func (s *Stream[T]) Filter(f FilterFunc[T]) *Stream[T] {
	var arr []T
	for _, v := range s.arr {
		if f(v) {
			arr = append(arr, v)
		}
	}
	s.arr = arr
	return s
}

func (s *Stream[T]) ToInt64Array() []int64 {
	var ret []int64
	for _, v := range s.transferArr {
		ret = append(ret, v.(int64))
	}
	return ret
}

func (s *Stream[T]) GroupBy(f GroupByFunc[T]) map[string][]T {
	ret := map[string][]T{}
	for _, v := range s.arr {
		if _, ok := ret[f(v)]; !ok {
			ret[f(v)] = []T{v}
		} else {
			ret[f(v)] = append(ret[f(v)], v)
		}
	}
	return ret
}

func (s *Stream[T]) ToStringMap(f ToStringMapValueFunc[T]) map[string]string {
	ret := make(map[string]string, len(s.arr))
	for _, el := range s.arr {
		k, v := f(el)
		ret[k] = v
	}
	return ret
}

func (s *Stream[T]) ToMap(f ToMapKeyFunc[T]) map[string]T {
	ret := make(map[string]T, len(s.arr))
	for _, v := range s.arr {
		ret[f(v)] = v
	}
	return ret
}

func (s *Stream[T]) Collect() []T {
	return s.arr
}

func (s *Stream[T]) CollectObject() []interface{} {
	return s.transferArr
}

type GroupByFunc[T any] func(t T) string
type FilterFunc[T any] func(t T) bool
type ToStringMapValueFunc[T any] func(t T) (string, string)
type ToMapKeyFunc[T any] func(t T) string
type MapFunc[T any] func(t T) interface{}

type JsonRpcCaller struct {
}

func (c *JsonRpcCaller) Call(cli *fetch.Client, req *fetch.Request) (ret map[string]interface{}, err error) {
	// var req *http.Request
	// if req, err = http.NewRequest("POST", url, bytes.NewReader([]byte(body))); err != nil {
	// 	return
	// }
	// req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Content-Type", "application/json")
	var resp *http.Response
	if resp, err = cli.Do(req, nil); err != nil {
		log.Printf("fail to call, url: %s, err: %s\n", req.URL.String(), err.Error())
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status code, url: %s , code: %d\n", req.URL.String(), resp.StatusCode)
		return
	}

	var b []byte
	if b, err = io.ReadAll(resp.Body); err != nil {
		return
	}

	if err = json.Unmarshal(b, &ret); err != nil {
		return
	}
	return
}

type KVNamespace string

func (n KVNamespace) New() (*cloudflare.KVNamespace, error) {
	return cloudflare.NewKVNamespace(string(n))
}

type WorkersHandler struct {
}

func (h *WorkersHandler) Error(w http.ResponseWriter, statusCode int, msg string, err error) {
	if err != nil {
		log.Println(err)
	}
	w.WriteHeader(statusCode)
	w.Write([]byte(msg))
}

func (h *WorkersHandler) InternalServerError(w http.ResponseWriter, err error) {
	h.Error(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError), err)
}

func TraverseField(values map[string]interface{}, fieldNames []string) (string, error) {
	if values == nil {
		return "", errors.New("values cannot be nil")
	}
	if len(fieldNames) == 0 {
		return "<no value>", nil
	}
	for k, v := range values {
		if k != fieldNames[0] {
			continue
		}
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Map {
			return TraverseField(v.(map[string]interface{}), fieldNames[1:])
		} else {
			if len(fieldNames) == 1 {
				if rv.Kind() == reflect.Int {
					return strconv.Itoa(v.(int)), nil
				} else if rv.Kind() == reflect.Bool {
					return strconv.FormatBool(v.(bool)), nil
				} else if rv.Kind() == reflect.Float64 {
					return strconv.FormatFloat(v.(float64), 'f', -1, 64), nil
				} else {
					return v.(string), nil
				}
			}
		}
	}
	return "<no value>", nil
}

func DebugPrintln(v interface{}) {
	jsonBytes, _ := json.Marshal(v)
	log.Println(string(jsonBytes))
}

// type D1Table[T any] struct {
// 	tableName string
// 	db        *sql.DB
// }

// func NewD1Table[T any](db *sql.DB, tableName string) *D1Table[T] {
// 	return &D1Table[T]{db: db, tableName: tableName}
// }

// func (t *D1Table[T]) QueryOne(filter string) (T, error) {
// 	arr, err := t.QueryBy(filter)
// 	var ret T
// 	if err != nil {
// 		return ret, err
// 	}
// 	return arr[0], nil
// }

// func (t *D1Table[T]) Insert(v T) (sql.Result, error) {
// 	rv := reflect.ValueOf(v)
// 	if rv.Kind() == reflect.Ptr {
// 		rv = rv.Elem()
// 	}
// 	rt := rv.Type()
// 	var fieldNames []string
// 	var fieldValues []interface{}
// 	for i := 0; i < rt.NumField(); i++ {
// 		field := rt.Field(i)
// 		tag := field.Tag.Get("json")
// 		if tag == "group" || tag == "service" || tag == "key" || tag == "value" {
// 			tag = fmt.Sprintf("`%s`", tag)
// 		}
// 		if tag == "id" {
// 			continue
// 		}
// 		fieldNames = append(fieldNames, tag)
// 		fieldValues = append(fieldValues, rv.Field(i).Interface())
// 	}
// 	var placeholders []string
// 	for i := 0; i < len(fieldNames); i++ {
// 		placeholders = append(placeholders, "?")
// 	}
// 	sqlStr := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, t.tableName, strings.Join(fieldNames, ", "), strings.Join(placeholders, ", "))
// 	log.Println(sqlStr)
// 	return t.db.Exec(sqlStr, fieldValues...)
// }

// func (t *D1Table[T]) QueryBy(filter string) ([]T, error) {
// 	sqlStr := fmt.Sprintf(`SELECT * FROM %s`, t.tableName)
// 	if filter != "" {
// 		sqlStr = fmt.Sprintf(`SELECT * FROM %s WHERE %s`, t.tableName, filter)
// 	}
// 	rows, err := t.db.Query(sqlStr)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if t.tableName == "secret_key" {
// 		sqlStr = fmt.Sprintf(`SELECT * FROM %s WHERE %s`, t.tableName, "access_key = 'REDACTED'")
// 	}
// 	log.Println(sqlStr)
// 	return t.parseRows(rows)
// }

// func (t *D1Table[T]) parseRows(rows *sql.Rows) ([]T, error) {
// 	var result []T
// 	var item T
// 	rt := reflect.TypeOf(item)
// 	resultKind := rt.Kind()
// 	if resultKind == reflect.Pointer {
// 		rt = rt.Elem()
// 	}
// 	for rows.Next() {
// 		values := make([]any, rt.NumField())
// 		scanValues := make([]any, rt.NumField())
// 		for i := range values {
// 			scanValues[i] = &values[i]
// 		}
// 		if err := rows.Scan(scanValues...); err != nil {
// 			return nil, err
// 		}
// 		rv := reflect.New(rt).Elem()
// 		for i := 0; i < rt.NumField(); i++ {
// 			fv := rv.FieldByName(rt.Field(i).Name)
// 			if fv.CanSet() {
// 				fv.Set(reflect.ValueOf(values[i]))
// 			}
// 		}
// 		if resultKind == reflect.Pointer {
// 			result = append(result, rv.Addr().Interface().(T))
// 		} else {
// 			result = append(result, rv.Interface().(T))
// 		}
// 	}
// 	return result, nil
// }

// func (t *D1Table[T]) UpdateBy(filter string, v T) (sql.Result, error) {
// 	rv := reflect.ValueOf(v)
// 	if rv.Kind() == reflect.Ptr {
// 		rv = rv.Elem()
// 	}
// 	rt := rv.Type()
// 	var fieldNames []string
// 	var fieldValues []interface{}
// 	for i := 0; i < rt.NumField(); i++ {
// 		field := rt.Field(i)
// 		tag := field.Tag.Get("json")
// 		if tag == "id" {
// 			continue
// 		}
// 		if tag == "group" || tag == "service" || tag == "key" || tag == "value" {
// 			tag = fmt.Sprintf("`%s`", tag)
// 		}
// 		fieldNames = append(fieldNames, tag)
// 		fieldValues = append(fieldValues, rv.Field(i).Interface())
// 	}

// 	var placeholders []string
// 	for _, fieldName := range fieldNames {
// 		placeholders = append(placeholders, fmt.Sprintf("%s = ?", fieldName))
// 	}
// 	sqlStr := fmt.Sprintf(`UPDATE %s SET %s WHERE %s`, t.tableName, strings.Join(placeholders, ", "), filter)
// 	log.Println(sqlStr)
// 	return t.db.Exec(sqlStr, fieldValues...)
// }

// func (t *D1Table[T]) DeleteBy(filter string) (sql.Result, error) {
// 	sqlStr := fmt.Sprintf(`DELETE FROM %s WHERE %s`, t.tableName, filter)
// 	log.Println(sqlStr)
// 	return t.db.Exec(sqlStr)
// }

// func (t *D1Table[T]) Count(filter string) (int64, error) {
// 	rows, err := t.db.Query(fmt.Sprintf("SELECT COUNT(*) FROM %s", t.tableName))
// 	if err != nil {
// 		return 0, err
// 	}
// 	rows.Next()
// 	var count int64
// 	return count, rows.Scan(&count)
// }

type Page struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func (p *Page) CalculateOffset(total int) (int, int, error) {
	if total < 0 || p.PageSize <= 0 || p.Page < 1 {
		return 0, 0, errors.New("invalid page params")
	}

	totalPages := (total + p.PageSize - 1) / p.PageSize
	if p.Page > totalPages {
		return 0, 0, &PageOutOfRangeError{}
	}

	offset := (p.Page - 1) * p.PageSize
	return offset, totalPages, nil
}

// type PageData[T any] struct {
// 	TotalPages int
// 	PageSize   int
// 	Page       int
// 	Data       []T
// }

type PageOutOfRangeError struct {
}

func (e *PageOutOfRangeError) Error() string {
	return "page out of range"
}

// func (t *D1Table[T]) PageBy(page *Page) (*PageData[T], error) {
// 	total, err := t.Count("")
// 	if err != nil {
// 		return nil, err
// 	}
// 	offset, totalPages, err := page.CalculateOffset(int(total))
// 	if err != nil {
// 		return nil, err
// 	}

// 	sqlStr := fmt.Sprintf(`SELECT * FROM %s LIMIT %d,%d`, t.tableName, offset, page.PageSize)
// 	log.Println(sqlStr)
// 	rows, err := t.db.Query(sqlStr)
// 	if err != nil {
// 		return nil, err
// 	}
// 	data, err := t.parseRows(rows)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &PageData[T]{TotalPages: totalPages, PageSize: page.PageSize, Page: page.Page, Data: data}, nil
// }

type KVCache struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt int64  `json:"created_at"`
}

type Rpc string

func (rpc Rpc) GetUrls() []string {
	return strings.Split(string(rpc), ",")
}

func (rpc Rpc) GetUrlsWithUnique() []string {
	if rpc == "" {
		return []string{}
	}
	return NewStringStream(rpc.GetUrls()).Unique()
}
