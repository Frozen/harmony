package stagedstreamsync

import (
	"context"

	"github.com/harmony-one/harmony/api/service/stagedstreamsync/kv"
	"github.com/pkg/errors"
)

type StageFinish struct {
	configs StageFinishCfg
}

type StageFinishCfg struct {
	db kv.RwDB
}

func NewStageFinish(cfg StageFinishCfg) *StageFinish {
	return &StageFinish{
		configs: cfg,
	}
}

func NewStageFinishCfg(db kv.RwDB) StageFinishCfg {
	return StageFinishCfg{
		db: db,
	}
}

func (finish *StageFinish) Exec(ctx context.Context, firstCycle bool, invalidBlockRevert bool, s *StageState, reverter Reverter, tx kv.RwTx) error {
	useInternalTx := tx == nil
	if useInternalTx {
		var err error
		tx, err = finish.configs.db.BeginRw(ctx)
		if err != nil {
			return errors.WithMessagef(err, "failed to begin tx")
		}
		defer tx.Rollback()
	}

	// TODO: prepare indices (useful for RPC) and finalize

	if useInternalTx {
		if err := tx.Commit(); err != nil {
			return errors.WithMessagef(err, "failed to commit tx")
		}
	}

	return nil
}

func (finish *StageFinish) Revert(ctx context.Context, firstCycle bool, u *RevertState, s *StageState, tx kv.RwTx) (err error) {
	useInternalTx := tx == nil
	if useInternalTx {
		tx, err = finish.configs.db.BeginRw(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}

	if err = u.Done(tx); err != nil {
		return err
	}

	if useInternalTx {
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (finish *StageFinish) CleanUp(ctx context.Context, firstCycle bool, p *CleanUpState, tx kv.RwTx) (err error) {
	useInternalTx := tx == nil
	if useInternalTx {
		tx, err = finish.configs.db.BeginRw(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}

	if useInternalTx {
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
