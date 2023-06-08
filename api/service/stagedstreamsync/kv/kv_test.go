package kv_test

import (
	"context"
	"testing"

	"github.com/harmony-one/harmony/api/service/stagedstreamsync/kv"
	"github.com/stretchr/testify/require"
)

func TestKV(t *testing.T) {
	ctx := context.TODO()
	t.Run("TestSimpleCase", func(t *testing.T) {
		db := kv.NewDB()
		rs, err := db.BeginRw(ctx)
		require.NoError(t, err)

		err = rs.Put("table1", []byte("key1"), []byte("value1"))
		require.NoError(t, err)

		t.Run("test-absense", func(t *testing.T) {
			val, err := rs.GetOne("table1", []byte("key2"))
			require.Error(t, err)
			require.Nil(t, val)
		})

		t.Run("test-existence", func(t *testing.T) {
			val, err := rs.GetOne("table1", []byte("key1"))
			require.NoError(t, err)
			require.NotNil(t, val)
		})
	})

	t.Run("TestTx", func(t *testing.T) {
		db := kv.NewDB()
		t.Run("TestFirstTx1", func(t *testing.T) {
			rs, err := db.BeginRw(ctx)
			require.NoError(t, err)

			rs.Put("table1", []byte("key1"), []byte("value1"))
			rs.Rollback()
		})
		t.Run("TestFirstTx2", func(t *testing.T) {
			rs, err := db.BeginRw(ctx)
			require.NoError(t, err)

			rs.Put("table1", []byte("key1"), []byte("value1"))
			rs.Rollback()
		})
		t.Run("TestFirstTx3", func(t *testing.T) {
			rs, err := db.BeginRw(ctx)
			require.NoError(t, err)

			rs.Put("table1", []byte("key1"), []byte("value1"))
			rs.Commit()
		})

		db.View(ctx, func(tx kv.Tx) error {
			val, err := tx.GetOne("table1", []byte("key1"))
			require.NoError(t, err)
			require.NotNil(t, val)
			return nil
		})

	})
}
