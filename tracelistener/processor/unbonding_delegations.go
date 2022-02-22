package processor

import (
	"bytes"

	"github.com/allinbits/tracelistener/tracelistener/processor/datamarshaler"
	"go.uber.org/zap"

	models "github.com/allinbits/demeris-backend-models/tracelistener"
	"github.com/allinbits/tracelistener/tracelistener"
)

type unbondingDelegationCacheEntry struct {
	delegator string
	validator string
}

type unbondingDelegationsProcessor struct {
	l                 *zap.SugaredLogger
	insertHeightCache map[unbondingDelegationCacheEntry]models.UnbondingDelegationRow
	deleteHeightCache map[unbondingDelegationCacheEntry]models.UnbondingDelegationRow
}

func (*unbondingDelegationsProcessor) TableSchema() string {
	return createUnbondingDelegationsTable
}

func (b *unbondingDelegationsProcessor) ModuleName() string {
	return "unbonding_delegations"
}

func (b *unbondingDelegationsProcessor) FlushCache() []tracelistener.WritebackOp {
	insert := make([]models.DatabaseEntrier, 0, len(b.insertHeightCache))
	deleteEntries := make([]models.DatabaseEntrier, 0, len(b.deleteHeightCache))

	if len(b.insertHeightCache) != 0 {
		for _, v := range b.insertHeightCache {
			insert = append(insert, v)
		}

		b.insertHeightCache = map[unbondingDelegationCacheEntry]models.UnbondingDelegationRow{}
	}

	if len(b.deleteHeightCache) == 0 && insert == nil {
		return nil
	}

	for _, v := range b.deleteHeightCache {
		deleteEntries = append(deleteEntries, v)
	}

	b.deleteHeightCache = map[unbondingDelegationCacheEntry]models.UnbondingDelegationRow{}

	return []tracelistener.WritebackOp{
		{
			DatabaseExec: insertUnbondingDelegation,
			Data:         insert,
		},
		{
			DatabaseExec: deleteUnbondingDelegation,
			Data:         deleteEntries,
		},
	}
}

func (b *unbondingDelegationsProcessor) OwnsKey(key []byte) bool {
	return bytes.HasPrefix(key, datamarshaler.UnbondingDelegationKey) ||
		bytes.HasPrefix(key, datamarshaler.UnbondingDelegationByValidatorKey)
}

func (b *unbondingDelegationsProcessor) Process(data tracelistener.TraceOperation) error {
	res, err := datamarshaler.NewDataMarshaler(b.l).UnbondingDelegations(data)
	if err != nil {
		return err
	}

	if res.Delegator == "" && res.Validator == "" {
		return nil // case in which this is an error operation, but the key wasn't UnbondingDelegationByValidatorKey
	}

	if data.Operation == tracelistener.DeleteOp.String() {
		b.deleteHeightCache[unbondingDelegationCacheEntry{
			validator: res.Validator,
			delegator: res.Delegator,
		}] = models.UnbondingDelegationRow{
			Delegator: res.Delegator,
			Validator: res.Validator,
		}

		return nil
	}

	b.insertHeightCache[unbondingDelegationCacheEntry{
		validator: res.Validator,
		delegator: res.Delegator,
	}] = models.UnbondingDelegationRow{
		Delegator: res.Delegator,
		Validator: res.Validator,
		Entries:   res.Entries,
	}

	return nil
}
