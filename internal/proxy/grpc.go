package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gogo/status"
	"github.com/mwitkow/grpc-proxy/proxy"
	"github.com/pundix/chain-gateway/internal/client"
	"github.com/pundix/chain-gateway/pkg/pocketbase"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

type GrpcProxier struct {
	Duration        time.Duration
	logger          *zap.Logger
	cli             *pocketbase.Client
	secretKeyCaches map[string]*client.SecretKey
	upstreamCaches  grpcUpstreamCaches
	secretMu        sync.RWMutex
}

func NewGrpc(cli *pocketbase.Client) *GrpcProxier {
	logger, _ := zap.NewDevelopment(zap.IncreaseLevel(zap.InfoLevel))
	return &GrpcProxier{
		logger:          logger,
		secretKeyCaches: make(map[string]*client.SecretKey),
		upstreamCaches:  make(grpcUpstreamCaches),
		cli:             cli,
		Duration:        5 * time.Minute,
	}
}

func (p *GrpcProxier) Fetch() {
	p.fetchUpstream()
	go func() {
		ticker := time.NewTicker(p.Duration)
		defer ticker.Stop()
		for {
			<-ticker.C
			p.fetchUpstream()
		}
	}()
}

func (p *GrpcProxier) Proxy() error {
	srv := grpc.NewServer(
		grpc.UnknownServiceHandler(proxy.TransparentHandler(p.director)),
		grpc.StreamInterceptor(
			p.authStreamInterceptor,
		),
	)

	grpc_health_v1.RegisterHealthServer(srv, &HealthServerImpl{})

	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		return err
	}

	errC := make(chan error)
	go func() {
		p.logger.Info("listening on", zap.String("address", lis.Addr().String()))
		if err := srv.Serve(lis); err != nil {
			errC <- err
		}
	}()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigC:
		srv.GracefulStop()
		p.logger.Info("grpc server stopped")
		return nil
	case err := <-errC:
		return err
	}
}

func (p *GrpcProxier) getChainId(md metadata.MD) (string, error) {
	chainId := md.Get("chainId")
	if len(chainId) == 0 {
		network := md.Get("network")
		if len(network) != 0 {
			switch network[0] {
			case "tron-testnet":
				chainId = []string{"3448148188"}
			case "tron-mainnet":
				chainId = []string{"728126428"}
			case "chihuahua-mainnet":
				chainId = []string{"chihuahua-1"}
			}
		}
	}
	if len(chainId) == 0 || chainId[0] == "" {
		return "", status.Errorf(codes.InvalidArgument, "chainId is empty")
	}
	return chainId[0], nil
}

func (p *GrpcProxier) director(ctx context.Context, fullMethodName string) (context.Context, grpc.ClientConnInterface, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	chainId, err := p.getChainId(md)
	if err != nil {
		return nil, nil, err
	}
	outCtx := metadata.NewOutgoingContext(ctx, md.Copy())

	var upstream *grpcUpstream
	var cc *grpc.ClientConn
	upstream, err = p.upstreamCaches.get(chainId)
	if upstream != nil {
		cc, err = upstream.get()
	}
	if err != nil {
		var sk *client.SecretKey
		p.secretMu.RLock()
		if vals := md.Get("accessKey"); len(vals) > 0 {
			sk = p.secretKeyCaches[vals[0]]
		}
		p.secretMu.RUnlock()
		if sk != nil {
			rt := NewRequestTraceBuilder(sk.Service, sk.Group).
				WithChainIdAndSource(chainId, "custom/grpc").
				WithRequest(md, fullMethodName).
				WithResponse(0, status.New(codes.Unavailable, err.Error())).Build()
			p.logger.Warn("get endpoint failed", zap.Any("request trace", rt))
		} else {
			p.logger.Warn("get endpoint failed", zap.Error(err))
		}
	}
	return outCtx, cc, err
}

func (p *GrpcProxier) fetchUpstream() {
	listResp, err := p.cli.ListRecords("ready_upstream", pocketbase.ListOptions{
		Filter: "protocol = 'grpc'",
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		p.logger.Error("fetch upstream failed", zap.Error(err))
		return
	}
	if listResp == nil || len(listResp.Items) == 0 {
		p.logger.Error("upstream not found")
		return
	}
	for _, record := range listResp.Items {
		var rpc []string
		for _, url := range record["rpc"].([]any) {
			rpc = append(rpc, url.(string))
		}
		chainId := record["chain_id"].(string)
		p.upstreamCaches.put(chainId, &grpcUpstream{
			chainId: chainId,
			rpc:     rpc,
			clis:    make(map[string]*grpc.ClientConn),
			next:    0,
			logger:  p.logger,
		}, p.loggingStreamInterceptor)
	}
	p.logger.Info("fetch upstream success", zap.Any("count", len(listResp.Items)))
}

func (p *GrpcProxier) loggingStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (gcs grpc.ClientStream, err error) {
	md, _ := metadata.FromIncomingContext(ctx)
	chainId, err := p.getChainId(md)
	if err != nil {
		return nil, err
	}
	var service, group string
	p.secretMu.RLock()
	if vals := md.Get("accesskey"); len(vals) > 0 {
		if sk, ok := p.secretKeyCaches[vals[0]]; ok && sk != nil {
			service = sk.Service
			group = sk.Group
		}
	}
	p.secretMu.RUnlock()
	if service == "" {
		service = "unknown"
	}
	if group == "" {
		group = "unknown"
	}
	requestTraceBuilder := NewRequestTraceBuilder(service, group).
		WithChainIdAndSource(chainId, "custom/grpc").
		WithUpstreamNode(cc.Target()).
		WithRequest(md, method)
	gcs, err = streamer(ctx, desc, cc, method)
	if err != nil {
		return nil, err
	}
	return newWrappedStream(gcs, requestTraceBuilder, p.logger), nil
}

func (p *GrpcProxier) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	md, _ := metadata.FromIncomingContext(ss.Context())
	accessKey := md.Get("accessKey")
	if len(accessKey) == 0 || !p.verifyAccessKey(accessKey[0]) {
		return status.Error(codes.Unauthenticated, codes.Unauthenticated.String())
	}
	return handler(srv, ss)
}

func (p *GrpcProxier) verifyAccessKey(accessKey string) bool {
	p.secretMu.RLock()
	if _, ok := p.secretKeyCaches[accessKey]; ok {
		p.secretMu.RUnlock()
		return true
	}
	p.secretMu.RUnlock()

	escaped := strings.ReplaceAll(accessKey, "'", "''")
	record, err := p.cli.GetFirstListItem("secret_key", pocketbase.ListOptions{
		Filter: fmt.Sprintf("access_key = '%s'", escaped),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		p.logger.Warn("get secret key failed", zap.Error(err))
		return false
	}

	if record != nil {
		p.secretMu.Lock()
		if _, ok := p.secretKeyCaches[accessKey]; ok {
			p.secretMu.Unlock()
			return true
		}
		p.secretKeyCaches[accessKey] = &client.SecretKey{
			AccessKey:    accessKey,
			SecretKey:    record["secret_key"].(string),
			Service:      record["service"].(string),
			Group:        record["group"].(string),
			AllowOrigins: record["allow_origins"].(string),
			AllowIps:     record["allow_ips"].(string),
		}
		p.secretMu.Unlock()
		return true
	}
	return false
}
