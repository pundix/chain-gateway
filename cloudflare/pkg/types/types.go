package types

import "strings"

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
