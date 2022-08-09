package rawdb

import (
	"github.com/ethereum/go-ethereum/ethdb"
	prom "github.com/harmony-one/harmony/api/service/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

var _ ethdb.KeyValueStore = &PrometheusWrapper{}

func init() {
	prom.PromRegistry().MustRegister(
		numReadsVec,
	)
}

var (
	numReadsVec = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hmy",
			Subsystem: "storage",
			Name:      "leveldb_num_reads",
			Help:      "number of reads",
		},
		[]string{"name", "type"},
	)
)

type PrometheusWrapper struct {
	db   ethdb.KeyValueStore
	name string
}

func NewPrometheusWrapper(db ethdb.KeyValueStore, name string) *PrometheusWrapper {
	return &PrometheusWrapper{db: db, name: name}
}

func (a PrometheusWrapper) Has(key []byte) (bool, error) {
	numReadsVec.With(prometheus.Labels{"type": "has", "name": a.name}).Inc()
	return a.db.Has(key)
}

func (a PrometheusWrapper) Get(key []byte) ([]byte, error) {
	numReadsVec.With(prometheus.Labels{"type": "get", "name": a.name}).Inc()
	return a.db.Get(key)
}

func (a PrometheusWrapper) Put(key []byte, value []byte) error {
	return a.db.Put(key, value)
}

func (a PrometheusWrapper) Delete(key []byte) error {
	return a.db.Delete(key)
}

func (a PrometheusWrapper) NewBatch() ethdb.Batch {
	return a.db.NewBatch()
}

func (a PrometheusWrapper) NewIterator() ethdb.Iterator {
	return a.db.NewIterator()
}

func (a PrometheusWrapper) NewIteratorWithStart(start []byte) ethdb.Iterator {
	return a.db.NewIteratorWithStart(start)
}

func (a PrometheusWrapper) NewIteratorWithPrefix(prefix []byte) ethdb.Iterator {
	return a.db.NewIteratorWithPrefix(prefix)
}

func (a PrometheusWrapper) Stat(property string) (string, error) {
	return a.db.Stat(property)
}

func (a PrometheusWrapper) Compact(start []byte, limit []byte) error {
	return a.db.Compact(start, limit)
}

func (a PrometheusWrapper) Close() error {
	return a.db.Close()
}
