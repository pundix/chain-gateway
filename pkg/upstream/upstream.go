package upstream

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	pkg_db "github.com/pundix/chain-gateway/pkg/db"
	"github.com/pundix/chain-gateway/pkg/types"
	_ "github.com/syumai/workers/cloudflare/d1"
)

type ReadyUpstreamGroup map[string][]pkg_db.ReadyUpstream

type RegistryApplyFunc func() RegistryApply

func Apply(db *sql.DB, applies ...RegistryApplyFunc) error {
	r, err := newRegistry(db)
	if err != nil {
		return err
	}
	r.apply(applies...)
	return nil
}

func newRegistry(db *sql.DB) (*upstreamRegistry, error) {
	return &upstreamRegistry{
		queries: pkg_db.New(db),
	}, nil
}

type upstreamRegistry struct {
	queries *pkg_db.Queries
}

func (r *upstreamRegistry) Register(source string, upstreams []pkg_db.Upstream) error {
	ctx := context.Background()
	if len(upstreams) == 0 {
		if err := r.queries.DelUpstreamBySource(ctx, source); err != nil {
			return err
		}
		if err := r.queries.DelReadyUpstreamBySource(ctx, source); err != nil {
			return err
		}
		return nil
	}

	dbUpstreams, err := r.queries.ListUpstreamsBySource(ctx, upstreams[0].Source)
	if err != nil {
		return err
	}
	dbUpstreamMap := types.NewArrayStream(dbUpstreams).ToMap(func(u pkg_db.Upstream) string {
		return u.ChainID
	})

	newUpstreams := types.NewArrayStream(upstreams).Filter(func(u pkg_db.Upstream) bool {
		_, ok := dbUpstreamMap[u.ChainID]
		return !ok
	}).Collect()

	for _, upstream := range newUpstreams {
		if _, err := r.queries.CreateUpstream(ctx, pkg_db.CreateUpstreamParams{
			ChainID:   upstream.ChainID,
			Source:    upstream.Source,
			Rpc:       upstream.Rpc,
			CreatedAt: upstream.CreatedAt,
		}); err != nil {
			return err
		}
		log.Printf("fetch new upstream %s from %s\n", upstream.ChainID, upstream.Source)
	}

	oldUpstreams := types.NewArrayStream(upstreams).Filter(func(u pkg_db.Upstream) bool {
		_, ok := dbUpstreamMap[u.ChainID]
		return ok
	}).Collect()

	for _, upstream := range oldUpstreams {
		dbUpstream := dbUpstreamMap[upstream.ChainID]
		if dbUpstream.Rpc != upstream.Rpc {
			if _, err := r.queries.UpdateUpstreamRpc(ctx, pkg_db.UpdateUpstreamRpcParams{
				Rpc:       upstream.Rpc,
				ChainID:   upstream.ChainID,
				Source:    upstream.Source,
				CreatedAt: upstream.CreatedAt,
			}); err != nil {
				return err
			}
			log.Printf("fetch old upstream %s from %s\n", upstream.ChainID, upstream.Source)
		}
	}

	upstreamMap := types.NewArrayStream(upstreams).ToMap(func(u pkg_db.Upstream) string {
		return u.ChainID
	})
	delUpstreams := types.NewArrayStream(dbUpstreams).Filter(func(u pkg_db.Upstream) bool {
		_, ok := upstreamMap[u.ChainID]
		return !ok
	}).Collect()

	for _, upstream := range delUpstreams {
		if err := r.queries.DelUpstreamByChainIdSource(ctx, pkg_db.DelUpstreamByChainIdSourceParams{
			ChainID: upstream.ChainID,
			Source:  upstream.Source,
		}); err != nil {
			return err
		}
		if err = r.queries.DelReadyUpstreamByChainIdSource(ctx, pkg_db.DelReadyUpstreamByChainIdSourceParams{
			ChainID: upstream.ChainID,
			Source:  upstream.Source,
		}); err != nil {
			return err
		}
		log.Printf("fetch delete upstream and readyUpstream %s from %s\n", upstream.ChainID, upstream.Source)
	}
	return nil
}

func (r *upstreamRegistry) Refresh(group ReadyUpstreamGroup) error {
	for source, readyUpstreams := range group {
		if len(readyUpstreams) == 0 {
			continue
		}
		dbUpstreams, err := r.queries.ListReadyUpstreamsBySource(context.Background(), source)
		if err != nil {
			return err
		}
		dbUpstreamMap := types.NewArrayStream(dbUpstreams).ToMap(func(u pkg_db.ReadyUpstream) string {
			return u.ChainID
		})

		newUpstreams := types.NewArrayStream(readyUpstreams).Filter(func(u pkg_db.ReadyUpstream) bool {
			_, ok := dbUpstreamMap[u.ChainID]
			return !ok
		}).Collect()

		for _, readyUpstream := range newUpstreams {
			if _, err := r.queries.CreateReadyUpstream(context.Background(), pkg_db.CreateReadyUpstreamParams{
				ChainID:   readyUpstream.ChainID,
				Source:    readyUpstream.Source,
				Rpc:       readyUpstream.Rpc,
				CreatedAt: readyUpstream.CreatedAt,
			}); err != nil {
				return err
			}
			log.Printf("refresh new ready upstream %s from %s\n", readyUpstream.ChainID, readyUpstream.Source)
		}

		oldUpstreams := types.NewArrayStream(readyUpstreams).Filter(func(u pkg_db.ReadyUpstream) bool {
			_, ok := dbUpstreamMap[u.ChainID]
			return ok
		}).Collect()

		for _, readyUpstream := range oldUpstreams {
			dbUpstream := dbUpstreamMap[readyUpstream.ChainID]
			if dbUpstream.Rpc != readyUpstream.Rpc {
				if _, err := r.queries.UpdateReadyUpstreamRpc(context.Background(), pkg_db.UpdateReadyUpstreamRpcParams{
					Rpc:       readyUpstream.Rpc,
					ChainID:   readyUpstream.ChainID,
					Source:    readyUpstream.Source,
					CreatedAt: readyUpstream.CreatedAt,
				}); err != nil {
					return err
				}
				log.Printf("refresh old ready upstream %s from %s\n", readyUpstream.ChainID, readyUpstream.Source)
			}
		}
	}
	return nil
}

type UpstreamNotFoundError struct {
}

func (e *UpstreamNotFoundError) Error() string {
	return fmt.Sprintf("upstream not found")
}

func (r *upstreamRegistry) apply(applyies ...RegistryApplyFunc) {
	for _, f := range applyies {
		f().Apply(r)
	}
}

func NewUpstreamWriter(db *sql.DB) (UpstreamPutRegistry, error) {
	return newRegistry(db)
}

type UpstreamPutRegistry interface {
	Register(source string, upstreams []pkg_db.Upstream) error
	Refresh(upstreamGroup ReadyUpstreamGroup) error
}

type RegistryApply interface {
	Apply(UpstreamPutRegistry)
}
