package processor

import (
	"bytes"

	models "github.com/allinbits/demeris-backend-models/tracelistener"

	host "github.com/cosmos/cosmos-sdk/x/ibc/core/24-host"
	"go.uber.org/zap"

	"github.com/allinbits/tracelistener/tracelistener"
	"github.com/allinbits/tracelistener/tracelistener/processor/datamarshaler"
)

type channelCacheEntry struct {
	channelID string
	portID    string
}

type ibcChannelsProcessor struct {
	l             *zap.SugaredLogger
	channelsCache map[channelCacheEntry]models.IBCChannelRow
}

func (*ibcChannelsProcessor) TableSchema() string {
	return createChannelsTable
}

func (b *ibcChannelsProcessor) ModuleName() string {
	return "ibc_channels"
}

func (b *ibcChannelsProcessor) FlushCache() []tracelistener.WritebackOp {
	if len(b.channelsCache) == 0 {
		return nil
	}

	l := make([]models.DatabaseEntrier, 0, len(b.channelsCache))

	for _, c := range b.channelsCache {
		l = append(l, c)
	}

	b.channelsCache = map[channelCacheEntry]models.IBCChannelRow{}

	return []tracelistener.WritebackOp{
		{
			DatabaseExec: insertChannel,
			Data:         l,
		},
	}
}

func (b *ibcChannelsProcessor) OwnsKey(key []byte) bool {
	return bytes.HasPrefix(key, []byte(host.KeyChannelEndPrefix))
}

func (b *ibcChannelsProcessor) Process(data tracelistener.TraceOperation) error {
	res, err := datamarshaler.NewDataMarshaler(b.l).IBCChannels(data)
	if err != nil {
		return err
	}

	b.channelsCache[channelCacheEntry{
		channelID: res.ChannelID,
		portID:    res.Port,
	}] = res

	return nil
}
