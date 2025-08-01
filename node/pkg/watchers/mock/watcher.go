package mock

import (
	"context"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/supervisor"
	eth_common "github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func NewWatcherRunnable(
	msgC chan<- *common.MessagePublication,
	obsvReqC <-chan *gossipv1.ObservationRequest,
	setC chan<- *common.GuardianSet,
	c *WatcherConfig,
) supervisor.Runnable {
	return func(ctx context.Context) error {
		logger := supervisor.Logger(ctx)
		supervisor.Signal(ctx, supervisor.SignalHealthy)

		logger.Info("Mock Watcher running.")

		for {
			select {
			case <-ctx.Done():
				logger.Info("Mock Watcher shutting down.")
				return nil
			case observation := <-c.MockObservationC:
				logger.Info("message observed", observation.ZapFields(zap.String("digest", observation.CreateDigest()))...)
				msgC <- observation //nolint:channelcheck // The channel to the processor is buffered and shared across chains, if it backs up we should stop processing new observations
			case gs := <-c.MockSetC:
				setC <- gs //nolint:channelcheck // Will only block this mock watcher
			case o := <-obsvReqC:
				hash := eth_common.BytesToHash(o.TxHash)
				logger.Info("Received obsv request", zap.String("log_msg_type", "obsv_req_received"), zap.String("tx_hash", hash.Hex()))
				msg, ok := c.ObservationDb[hash]
				if ok {
					msg2 := *msg
					msg2.IsReobservation = true
					msgC <- &msg2 //nolint:channelcheck // The channel to the processor is buffered and shared across chains, if it backs up we should stop processing new observations
				}
			}
		}
	}
}
