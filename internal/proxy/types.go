package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/status"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

type RequestTrace struct {
	Protocol string `json:"protocol"`
	// ID        interface{} `json:"id"`
	Method    string     `json:"method"`
	ChainId   string     `json:"chainId"`
	Source    string     `json:"source"`
	Url       string     `json:"url"`
	Latency   int64      `json:"latency"`
	Group     string     `json:"group"`
	Service   string     `json:"service"`
	Status    codes.Code `json:"status"`
	Message   string     `json:"message"`
	VisitorIp string     `json:"visitorIp"`
}

func (rt *RequestTrace) Println() {
	bytes, _ := json.Marshal(rt)
	fmt.Println(string(bytes))
}

type RequestTraceBuilder struct {
	rt *RequestTrace
}

func NewRequestTraceBuilder(service, group string) *RequestTraceBuilder {
	return &RequestTraceBuilder{
		rt: &RequestTrace{
			Protocol: "grpc",
			Service:  service,
			Group:    group,
		},
	}
}

func (b *RequestTraceBuilder) WithResponse(latency int64, status *status.Status) *RequestTraceBuilder {
	b.rt.Status = status.Code()
	b.rt.Message = status.Message()
	b.rt.Latency = latency
	return b
}

func (b *RequestTraceBuilder) WithUpstreamNode(url string) *RequestTraceBuilder {
	b.rt.Url = url
	return b
}

func (b *RequestTraceBuilder) WithChainIdAndSource(chainId, source string) *RequestTraceBuilder {
	b.rt.ChainId = chainId
	b.rt.Source = source
	return b
}

func (b *RequestTraceBuilder) WithRequest(md metadata.MD, method string) *RequestTraceBuilder {
	b.rt.Method = method
	ip := md.Get("x-forwarded-for")
	if len(ip) > 0 {
		b.rt.VisitorIp = ip[0]
		return b
	}
	ip = md.Get("x-real-ip")
	if len(ip) > 0 {
		b.rt.VisitorIp = ip[0]
	}
	return b
}

func (b *RequestTraceBuilder) Build() *RequestTrace {
	return b.rt
}

type wrappedStream struct {
	grpc.ClientStream
	logger *zap.Logger
	rtb    *RequestTraceBuilder
	start  time.Time
}

func newWrappedStream(s grpc.ClientStream, rtb *RequestTraceBuilder, logger *zap.Logger) grpc.ClientStream {
	return &wrappedStream{
		ClientStream: s,
		logger:       logger,
		rtb:          rtb,
	}
}

func (w *wrappedStream) RecvMsg(m interface{}) error {
	err := w.ClientStream.RecvMsg(m)
	callStatus := status.New(codes.OK, codes.OK.String())
	if err != nil {
		if status, ok := status.FromError(err); ok {
			callStatus = status
		} else {
			return err
		}
	}
	rt := w.rtb.WithResponse(time.Since(w.start).Milliseconds(), callStatus).Build()
	w.logger.Info("reached endpoint", zap.Any("request trace", rt))
	return nil
}

func (w *wrappedStream) SendMsg(m interface{}) error {
	w.start = time.Now()
	return w.ClientStream.SendMsg(m)
}

type HealthServerImpl struct{}

func (h *HealthServerImpl) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (h *HealthServerImpl) List(ctx context.Context, req *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (h *HealthServerImpl) Watch(req *grpc_health_v1.HealthCheckRequest, srv grpc_health_v1.Health_WatchServer) error {
	return srv.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	})
}

type grpcUpstream struct {
	chainId string
	rpc     []string
	clis    map[string]*grpc.ClientConn
	next    uint32
	mu      sync.RWMutex
	logger  *zap.Logger
}

func (u *grpcUpstream) get() (*grpc.ClientConn, error) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if len(u.rpc) == 0 {
		return nil, errors.New("zero endpoints")
	}
	idx := int(atomic.AddUint32(&u.next, 1)) % len(u.rpc)
	url := u.rpc[idx]
	conn, ok := u.clis[url]
	if !ok || conn == nil {
		return nil, fmt.Errorf("no client for url: %s", url)
	}
	return conn, nil
}

func (u *grpcUpstream) refresh(rpc []string, loggingStreamInterceptor grpc.StreamClientInterceptor) {
	u.mu.Lock()
	newSet := make(map[string]bool, len(rpc))
	for _, r := range rpc {
		newSet[r] = true
	}

	var toAdd, toDel []string
	for _, r := range rpc {
		if _, ok := u.clis[r]; !ok {
			toAdd = append(toAdd, r)
		}
	}

	for _, r := range u.rpc {
		if !newSet[r] {
			toDel = append(toDel, r)
		}
	}
	u.mu.Unlock()

	clis, err := u.new(toAdd, loggingStreamInterceptor)
	if err != nil {
		u.logger.Error("refresh upstream failed", zap.Error(err), zap.String("chainId", u.chainId))
		return
	}

	u.mu.Lock()
	u.rpc = append([]string(nil), rpc...)
	for url, conn := range clis {
		u.clis[url] = conn
	}
	u.mu.Unlock()

	if len(toDel) > 0 {
		go func(urls []string) {
			time.Sleep(5 * time.Second)
			u.close(urls)
		}(toDel)
	}
}

func (u *grpcUpstream) new(rpc []string, loggingStreamInterceptor grpc.StreamClientInterceptor) (map[string]*grpc.ClientConn, error) {
	clis := make(map[string]*grpc.ClientConn, len(rpc))
	for _, url := range rpc {
		var creds credentials.TransportCredentials
		if strings.Contains(url, "443") {
			creds = credentials.NewTLS(&tls.Config{})
		} else {
			creds = insecure.NewCredentials()
		}
		conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(creds), grpc.WithStreamInterceptor(loggingStreamInterceptor))
		if err != nil {
			u.logger.Error("create grpc client failed", zap.Error(err), zap.String("url", url), zap.String("chainId", u.chainId))
			return nil, err
		}
		clis[url] = conn
	}
	return clis, nil
}

func (u *grpcUpstream) close(rpc []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, url := range rpc {
		if conn, ok := u.clis[url]; ok && conn != nil {
			conn.Close()
			delete(u.clis, url)
		}
	}
}

type grpcUpstreamCaches map[string]*grpcUpstream

// package-level mutex for protecting concurrent access to checkCaches (map)
var grpcUpstreamCachesMu sync.RWMutex

func (guc grpcUpstreamCaches) get(chainId string) (*grpcUpstream, error) {
	grpcUpstreamCachesMu.RLock()
	upstream, ok := guc[chainId]
	grpcUpstreamCachesMu.RUnlock()
	if ok {
		return upstream, nil
	}
	return nil, errors.New("no upstream found")
}

func (guc grpcUpstreamCaches) put(chainId string, value *grpcUpstream, loggingStreamInterceptor grpc.StreamClientInterceptor) {
	grpcUpstreamCachesMu.Lock()
	upstream, ok := guc[chainId]
	grpcUpstreamCachesMu.Unlock()
	if !ok {
		value.refresh(value.rpc, loggingStreamInterceptor)
		grpcUpstreamCachesMu.Lock()
		guc[chainId] = value
		grpcUpstreamCachesMu.Unlock()
		return
	}
	upstream.refresh(value.rpc, loggingStreamInterceptor)
}
