package collection

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pundix/chain-gateway/internal/client"
)

type Collection interface {
	Apply(core.App, *client.ChainGatewayClient)
}
