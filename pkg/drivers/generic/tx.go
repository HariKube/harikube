package generic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/k3s-io/kine/pkg/metrics"
	"github.com/k3s-io/kine/pkg/server"
	"github.com/k3s-io/kine/pkg/util"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type contextKey int

const (
	txKey contextKey = iota
)

// explicit interface check
var _ server.Transaction = (*Tx)(nil)

type generic interface {
	execute(context.Context, string, ...interface{}) (sql.Result, error)
	query(context.Context, string, ...interface{}) (*sql.Rows, error)
	queryRow(context.Context, string, ...interface{}) *sql.Row
}

type Tx struct {
	x *sql.Tx
	d *Generic
}

func (d *Generic) BeginTx(ctx context.Context, opts *sql.TxOptions) (server.Transaction, error) {
	logrus.Tracef("TX BEGIN")
	x, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{
		x: x,
		d: d,
	}, nil
}

func (t *Tx) Commit() error {
	logrus.Tracef("TX COMMIT")
	return t.x.Commit()
}

func (t *Tx) MustCommit() {
	if err := t.Commit(); err != nil {
		logrus.Fatalf("Transaction commit failed: %v", err)
	}
}

func (t *Tx) Rollback() error {
	logrus.Tracef("TX ROLLBACK")
	return t.x.Rollback()
}

func (t *Tx) MustRollback() {
	if err := t.Rollback(); err != nil {
		if err != sql.ErrTxDone {
			logrus.Fatalf("Transaction rollback failed: %v", err)
		}
	}
}

func (t *Tx) GetCompactRevision(ctx context.Context) (int64, error) {
	var id int64
	row := t.queryRow(ctx, compactRevSQL)
	err := row.Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (t *Tx) SetCompactRevision(ctx context.Context, revision int64) error {
	logrus.Tracef("TX SETCOMPACTREVISION %v", revision)
	_, err := t.execute(ctx, t.d.UpdateCompactSQL, revision)
	return err
}

func (t *Tx) Compact(ctx context.Context, revision int64) (int64, error) {
	logrus.Tracef("TX COMPACT %v", revision)
	res, err := t.execute(ctx, t.d.CompactSQL, revision, revision)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (t *Tx) DeleteRevision(ctx context.Context, revision int64) error {
	logrus.Tracef("TX DELETEREVISION %v", revision)
	_, err := t.execute(ctx, t.d.DeleteSQL, revision)
	return err
}

func (t *Tx) CurrentRevision(ctx context.Context) (int64, error) {
	var id int64
	row := t.queryRow(ctx, revSQL)
	err := row.Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (t *Tx) query(ctx context.Context, sql string, args ...any) (result *sql.Rows, err error) {
	logrus.Tracef("TX QUERY %v : %s", util.Summarize(args), util.Stripped(sql))
	startTime := time.Now()
	defer func() {
		metrics.ObserveSQL(startTime, t.d.ErrCode(err), util.Stripped(sql), args)
	}()
	return t.x.QueryContext(ctx, sql, args...)
}

func (t *Tx) queryRow(ctx context.Context, sql string, args ...any) (result *sql.Row) {
	logrus.Tracef("TX QUERY ROW %v : %s", util.Summarize(args), util.Stripped(sql))
	startTime := time.Now()
	defer func() {
		metrics.ObserveSQL(startTime, t.d.ErrCode(result.Err()), util.Stripped(sql), args)
	}()
	return t.x.QueryRowContext(ctx, sql, args...)
}

func (t *Tx) execute(ctx context.Context, sql string, args ...any) (result sql.Result, err error) {
	logrus.Tracef("TX EXEC %v : %s", util.Summarize(args), util.Stripped(sql))
	startTime := time.Now()
	defer func() {
		metrics.ObserveSQL(startTime, t.d.ErrCode(err), util.Stripped(sql), args)
	}()
	return t.x.ExecContext(ctx, sql, args...)
}

//nolint:revive
func (t *Tx) InsertMetadata(ctx context.Context, id int64, key string, createRevision int64, value, prevValue []byte, obj runtime.Object, uid types.UID, labels map[string]string, fieldsSet fields.Set, owners []metav1.OwnerReference, finalizers []string, delete bool) (err error) {
	metadataSQLs := []struct {
		sql  string
		args []any
	}{}

	foreground := len(finalizers) == 1 && finalizers[0] == metav1.FinalizerDeleteDependents
	foregroundGCNeeded := map[string]bool{}

	if !delete {
		var prevValueObj *unstructured.Unstructured = nil
		prevValueDecodeOnce := sync.Once{}
		prevValueDecode := func() error {
			if len(prevValue) == 0 {
				return nil
			}

			prevValueDecodeOnce.Do(func() {
				prevValueObj = &unstructured.Unstructured{}
				if _, _, err := unstructuredDecoder.Decode(prevValue, nil, prevValueObj); err != nil {
					prevValueObj = nil
				}
			})

			if prevValueObj == nil {
				return errors.New("failed to decode previous value")
			}

			return nil
		}

		for _, owner := range owners {
			if _, ok := labels["skip-controller-manager-metadata-caching"]; ok && (owner.BlockOwnerDeletion == nil || !*owner.BlockOwnerDeletion) {
				if err := prevValueDecode(); err != nil {
					return err
				}

				if prevValueObj != nil {
					preBlocks := map[types.UID]bool{}
					for i := range prevValueObj.GetOwnerReferences() {
						if prevValueObj.GetOwnerReferences()[i].BlockOwnerDeletion != nil && *prevValueObj.GetOwnerReferences()[i].BlockOwnerDeletion {
							preBlocks[prevValueObj.GetOwnerReferences()[i].UID] = true
						}
					}

					for i := range owners {
						if _, ok := preBlocks[owners[i].UID]; ok && (owners[i].BlockOwnerDeletion == nil || !*owners[i].BlockOwnerDeletion) {
							foregroundGCNeeded[string(owner.UID)] = true
						}
					}
				}
			}

			metadataSQLs = append(metadataSQLs, struct {
				sql  string
				args []any
			}{
				sql:  t.d.InsertOwnerSQL,
				args: []any{id, owner.UID, owner.BlockOwnerDeletion},
			})
		}

		for k, v := range labels {
			metadataSQLs = append(metadataSQLs, struct {
				sql  string
				args []any
			}{
				sql:  t.d.InsertLabelSQL,
				args: []any{id, key, k, v},
			})
		}

		if len(fieldsSet) != 0 {
			fieldsMap := map[string]string{}
			for k, v := range fieldsSet {
				fieldsMap[strings.ReplaceAll(k, ".", "_")] = v
			}

			var jsonData string
			if jsonData, err = jsoniter.MarshalToString(fieldsMap); err != nil {
				return err
			}

			metadataSQLs = append(metadataSQLs, struct {
				sql  string
				args []any
			}{
				sql:  t.d.InsertFieldsSQL,
				args: []any{id, key, jsonData},
			})
		}

		if foreground {
			delete = true
		}
	}

	if _, ok := labels["skip-controller-manager-metadata-caching"]; ok && delete {

		orphan := len(finalizers) == 1 && finalizers[0] == metav1.FinalizerOrphanDependents

		rows, err := t.query(ctx, t.d.GetOwnedSQL, uid)
		if err != nil {
			if err != sql.ErrNoRows {
				return err
			}
		}
		defer rows.Close()

		foregroundDeletionBlocked := false
		ownedCount := 0
		for rows.Next() {
			ownedCount++
			var ownedKey, ownedUID string
			var ownedValue []byte
			var ownedId, ownedCreateRevision int64
			var ownedBlockOwnerDeletion bool
			if err = rows.Scan(&ownedId, &ownedKey, &ownedUID, &ownedCreateRevision, &ownedValue, &ownedBlockOwnerDeletion); err != nil {
				return err
			} else if ownedId == 0 {
				continue
			}

			ownedObj := &unstructured.Unstructured{}
			if _, _, err := unstructuredDecoder.Decode(ownedValue, nil, ownedObj); err != nil {
				return err
			}

			if orphan {
				ownersToDelete := []int{}
				for i := range ownedObj.GetOwnerReferences() {
					if ownedObj.GetOwnerReferences()[i].UID == uid {
						ownersToDelete = append(ownersToDelete, i)
					}
				}

				for _, i := range ownersToDelete {
					ownedObj.SetOwnerReferences(append(ownedObj.GetOwnerReferences()[:i], ownedObj.GetOwnerReferences()[i+1:]...))
				}

				ownedNewValue, err := jsoniter.Marshal(ownedObj)
				if err != nil {
					return err
				}

				if t.d.LastInsertID {
					metadataSQLs = append(metadataSQLs, struct {
						sql  string
						args []any
					}{
						sql:  t.d.InsertLastInsertIDSQL,
						args: []any{ownedKey, ownedUID, 0, 0, ownedCreateRevision, ownedId, 0, ownedNewValue, ownedValue},
					})
				} else {
					if err := t.queryRow(ctx, t.d.InsertSQL, ownedKey, ownedUID, 0, 0, ownedCreateRevision, ownedId, 0, ownedNewValue, ownedValue).Err(); err != nil {
						return err
					}
				}
			} else if len(ownedObj.GetFinalizers()) == 0 {
				if _, err := t.d.Insert(context.WithValue(ctx, txKey, t), ownedKey, false, true, ownedCreateRevision, ownedId, 0, nil, ownedValue); err != nil {
					return err
				}
			} else if ownedObj.GetDeletionTimestamp() != nil && foreground && ownedBlockOwnerDeletion {
				foregroundDeletionBlocked = true
			} else if ownedObj.GetDeletionTimestamp() == nil {
				if foreground && ownedBlockOwnerDeletion {
					foregroundDeletionBlocked = true
				}

				ownedObj.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})

				ownedNewValue, err := jsoniter.Marshal(ownedObj)
				if err != nil {
					return err
				}

				if t.d.LastInsertID {
					metadataSQLs = append(metadataSQLs, struct {
						sql  string
						args []any
					}{
						sql:  t.d.InsertLastInsertIDSQL,
						args: []any{ownedKey, ownedUID, 0, 0, ownedCreateRevision, ownedId, 0, ownedNewValue, ownedValue},
					})
				} else {
					if err := t.queryRow(ctx, t.d.InsertSQL, ownedKey, ownedUID, 0, 0, ownedCreateRevision, ownedId, 0, ownedNewValue, ownedValue).Err(); err != nil {
						return err
					}
				}
			}
		}

		if foreground && !foregroundDeletionBlocked {
			if _, err := t.d.Insert(context.WithValue(ctx, txKey, t), key, false, true, createRevision, id, 0, nil, value); err != nil {
				return err
			}
		}

		if !foreground || (foreground && !foregroundDeletionBlocked) {
			for _, owner := range owners {
				foregroundGCNeeded[string(owner.UID)] = true
			}
		}
	}

	for ownerUID := range foregroundGCNeeded {
		rows, err := t.query(ctx, t.d.GetUIDSQL, ownerUID)
		if err != nil {
			if err != sql.ErrNoRows {
				return err
			}
		}
		defer rows.Close()

		for rows.Next() {
			var ownerId int64
			var ownerKey string
			var ownerDeleted bool
			var ownerCreateRev int64
			var ownerValue []byte
			if err = rows.Scan(&ownerId, &ownerKey, &ownerDeleted, &ownerCreateRev, &ownerValue); err != nil {
				return err
			} else if ownerDeleted {
				continue
			}

			ownerObj := &unstructured.Unstructured{}
			if _, _, err := unstructuredDecoder.Decode(ownerValue, nil, ownerObj); err != nil {
				return err
			}

			if len(ownerObj.GetFinalizers()) != 1 || ownerObj.GetFinalizers()[0] != metav1.FinalizerDeleteDependents {
				continue
			}

			ownedRows, err := t.query(ctx, t.d.GetOwnedSQL, ownerUID)
			if err != nil {
				if err != sql.ErrNoRows {
					return err
				}
			}
			defer ownedRows.Close()

			foregroundDeletionUnblocked := true
			for rows.Next() {
				var ownedKey, ownedUID string
				var ownedValue []byte
				var ownedId, ownedCreateRevision int64
				var ownedBlockOwnerDeletion bool
				if err = rows.Scan(&ownedId, &ownedKey, &ownedUID, &ownedCreateRevision, &ownedValue, &ownedBlockOwnerDeletion); err != nil {
					return err
				} else if ownedId == 0 {
					continue
				}

				ownedObj := &unstructured.Unstructured{}
				if _, _, err := unstructuredDecoder.Decode(ownedValue, nil, ownedObj); err != nil {
					return err
				}

				for _, ownerRef := range ownedObj.GetOwnerReferences() {
					if string(ownerRef.UID) == ownerUID && ownerRef.BlockOwnerDeletion != nil && *ownerRef.BlockOwnerDeletion {
						foregroundDeletionUnblocked = false
					}
				}
			}

			if foregroundDeletionUnblocked {
				if _, err := t.d.Insert(context.WithValue(ctx, txKey, t), ownerKey, false, true, ownerCreateRev, ownerId, 0, nil, ownerValue); err != nil {
					return err
				}
			}
		}
	}

	for _, meta := range metadataSQLs {
		if _, err = t.execute(ctx, meta.sql, meta.args...); err != nil {
			return err
		}
	}

	return
}
