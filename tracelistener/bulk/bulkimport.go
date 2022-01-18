package bulk

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/allinbits/tracelistener/tracelistener/database"

	types2 "github.com/cosmos/cosmos-sdk/store/types"

	"github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	db2 "github.com/tendermint/tm-db"

	"go.uber.org/zap"

	"github.com/allinbits/tracelistener/tracelistener"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type Importer struct {
	Path         string
	TraceWatcher tracelistener.TraceWatcher
	Processor    tracelistener.DataProcessor
	Logger       *zap.SugaredLogger
	Database     *database.Instance
	Modules      []string
}

var moduleList = map[string]struct{}{
	"bank":         {},
	"ibc":          {},
	"staking":      {},
	"distribution": {},
	"transfer":     {},
	"acc":          {},
}

func ImportableModulesList() []string {
	ml := make([]string, 0, len(moduleList))
	for k := range moduleList {
		ml = append(ml, k)
	}

	return ml
}

func (i Importer) validateModulesList() error {
	for _, m := range i.Modules {
		if _, ok := moduleList[m]; !ok {
			return fmt.Errorf("unknown bulk import module %s", m)
		}
	}

	return nil
}

func (i *Importer) Do() error {
	if err := i.validateModulesList(); err != nil {
		return err
	}

	if i.Modules == nil {
		i.Modules = ImportableModulesList()
	}

	importingWg := sync.WaitGroup{}
	t0 := time.Now()
	// spawn a goroutine that logs errors from processor's error chan
	go func() {
		for {
			select {
			case e := <-i.Processor.ErrorsChan():
				te := e.(tracelistener.TracingError)
				i.Logger.Errorw(
					"error while processing data",
					"error", te.InnerError,
					"data", te.Data,
					"moduleName", te.Module)
			case b := <-i.Processor.WritebackChan():
				importingWg.Add(1)
				for _, p := range b {
					for _, asd := range p.Data {
						i.Logger.Debugw("writeback unit", "data", asd)
					}

					is := p.InterfaceSlice()
					if len(is) == 0 {
						continue
					}

					if err := i.Database.Add(p.DatabaseExec, is); err != nil {
						i.Logger.Error("database error ", err)
					}
				}

				i.Logger.Debugw("finished processing writeback data")
				importingWg.Done()
			}
		}
	}()

	i.Path = strings.TrimSuffix(i.Path, ".db")

	dbName := filepath.Base(i.Path)
	path := filepath.Dir(i.Path)

	db, err := db2.NewGoLevelDBWithOpts(dbName, path, &opt.Options{
		ErrorIfMissing: true,
		ReadOnly:       true,
	})

	if err != nil {
		return fmt.Errorf("cannot open chain database, %w", err)
	}
	rm := rootmulti.NewStore(db)
	keys := make([]types2.StoreKey, 0, len(i.Modules))

	for _, ci := range i.Modules {
		key := types.NewKVStoreKey(ci)
		keys = append(keys, key)
		rm.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	}

	if err := rm.LoadLatestVersion(); err != nil {
		panic(err)
	}

	processingTime := time.Now()

	keysLen := len(keys)
	for idx, key := range keys {
		i.Logger.Infow("processing started", "module", key.Name(), "index", idx+1, "total", keysLen)

		store := rm.GetKVStore(key)
		ii := store.Iterator(nil, nil)

		writtenIdx := 0
		for ; ii.Valid(); ii.Next() {
			writtenIdx++

			to := tracelistener.TraceOperation{
				Operation: tracelistener.WriteOp.String(),
				Key:       ii.Key(),
				Value:     ii.Value(),
			}

			if writtenIdx == 1000 {
				time.Sleep(1 * time.Second)
				if err := i.Processor.Flush(); err != nil {
					return fmt.Errorf("cannot flush processor cache, %w", err)
				}
				writtenIdx = 0
			}

			if err := i.TraceWatcher.ParseOperation(to); err != nil {
				return fmt.Errorf("cannot parse operation %v, %w", to, err)
			}

			i.Logger.Debugw("parsed data", "key", string(to.Key), "value", string(to.Value))
		}

		if err := ii.Error(); err != nil {
			return fmt.Errorf("iterator error, %w", err)
		}

		if err := ii.Close(); err != nil {
			return fmt.Errorf("cannot close iterator, %w", err)
		}

		time.Sleep(1 * time.Second)

		if err := i.Processor.Flush(); err != nil {
			return fmt.Errorf("cannot flush processor cache, %w", err)
		}

		i.Logger.Infow("processing done", "module", key.Name(), "index", idx+1, "total", keysLen)
	}

	if err := db.Close(); err != nil {
		return fmt.Errorf("database closing error, %w", err)
	}

	importingWg.Wait()
	tn := time.Now()
	i.Logger.Infow("import done", "total time", tn.Sub(t0), "processing time", tn.Sub(processingTime))

	return nil
}
