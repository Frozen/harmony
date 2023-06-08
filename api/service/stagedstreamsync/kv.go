package stagedstreamsync

import "context"

type RwDB interface {
	// View is read-only transaction.
	View(ctx context.Context, f func(tx Tx) error) error
}

// Tx is a transaction.
type Tx interface {
}
