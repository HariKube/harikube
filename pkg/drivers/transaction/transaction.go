package transaction

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/k3s-io/kine/pkg/server"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	kjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
)

var (
	ErrMissingValue           = errors.New("missing value")
	ErrMissingSpecs           = errors.New("missing specs")
	ErrMissingResources       = errors.New("missing resources")
	ErrMissingMetadata        = errors.New("missing metadata")
	ErrMissingResourceVersion = errors.New("missing resourceVersion")

	unstructuredDecoder = kjson.NewSerializerWithOptions(
		kjson.DefaultMetaFactory,
		unstructuredscheme.NewUnstructuredCreator(),
		unstructuredscheme.NewUnstructuredObjectTyper(),
		kjson.SerializerOptions{Yaml: true, Pretty: false, Strict: false},
	)
)

func New(backend server.Backend) server.Backend {
	return &transaction{
		backend: backend,
	}
}

type transaction struct {
	backend server.Backend
}

func (t *transaction) Start(ctx context.Context) error {
	return t.backend.Start(ctx)
}

func (t *transaction) Get(ctx context.Context, key string, revision int64, keysOnly bool) (int64, *server.KeyValue, error) {
	return t.backend.Get(ctx, key, revision, keysOnly)
}

func (t *transaction) Create(ctx context.Context, key string, value []byte, lease int64) (int64, error) {
	if !strings.HasPrefix(key, "/harikube/transaction") {
		return t.backend.Create(ctx, key, value, lease)
	} else if len(value) == 0 {
		return 0, errors.Join(ErrMissingValue, server.ErrNotSupported)
	}

	currRev, err := t.backend.CurrentRevision(ctx)
	if err != nil {
		return 0, errors.Join(err, server.ErrGRPCUnhealthy)
	}

	obj := &unstructured.Unstructured{}
	if _, _, err := unstructuredDecoder.Decode(value, nil, obj); err != nil {
		return 0, errors.Join(err, server.ErrNotSupported)
	}

	specs, ok := obj.Object["specs"]
	if !ok {
		return currRev, errors.Join(ErrMissingSpecs, server.ErrNotSupported)
	}

	resources, ok := specs.(map[string]interface{})["resources"]
	if !ok {
		return currRev, errors.Join(ErrMissingResources, server.ErrNotSupported)
	}

	rawResources, ok := resources.(map[string][]byte)
	if !ok {
		return currRev, errors.Join(ErrMissingResources, server.ErrNotSupported)
	}

	tx, err := t.backend.Transaction(ctx)
	if err != nil {
		return currRev, errors.Join(err, server.ErrGRPCUnhealthy)
	}

	ctx = context.WithValue(ctx, server.TransactionKey, tx)

	maxRev := currRev
	var txErr error
LOOP:
	for key := range rawResources {
		obj := &unstructured.Unstructured{}
		if _, _, err := unstructuredDecoder.Decode(rawResources[key], nil, obj); err != nil {
			txErr = rpctypes.ErrGRPCTooManyOps

			break LOOP
		}

		metadata, ok := obj.Object["metadata"]
		if !ok {
			txErr = errors.Join(ErrMissingMetadata, rpctypes.ErrGRPCTooManyOps)

			break LOOP
		}

		resourceVersion, ok := metadata.(map[string]interface{})["resourceVersion"]
		if !ok {
			txErr = errors.Join(ErrMissingResourceVersion, rpctypes.ErrGRPCTooManyOps)

			break LOOP
		}

		revision, err := strconv.ParseInt(resourceVersion.(string), 10, 64)
		if err != nil {
			txErr = errors.Join(ErrMissingResourceVersion, rpctypes.ErrGRPCTooManyOps)

			break LOOP
		}

		keyParts := strings.Split(key, "#")
		if len(keyParts) != 2 {
			txErr = rpctypes.ErrGRPCKeyNotFound

			break LOOP
		}

		switch keyParts[1] {
		case "create":
			r, err := t.backend.Create(ctx, keyParts[0], rawResources[key], lease)
			if err != nil {
				txErr = err

				break LOOP
			}

			maxRev = max(maxRev, r)
		case "update":
			r, _, _, err := t.backend.Update(ctx, keyParts[0], rawResources[key], revision, lease)
			if err != nil {
				txErr = err

				break LOOP
			}

			maxRev = max(maxRev, r)
		case "delete":
			r, _, _, err := t.backend.Delete(ctx, keyParts[0], revision)
			if err != nil {
				txErr = err

				break LOOP
			}

			maxRev = max(maxRev, r)
		default:
			txErr = server.ErrNotSupported

			break LOOP
		}
	}

	if txErr == nil {
		if err := tx.Rollback(); err != nil {
			return currRev, errors.Join(txErr, err, rpctypes.ErrGRPCTooManyOps)
		}

		return currRev, errors.Join(txErr, rpctypes.ErrGRPCTooManyOps)
	}

	if err := tx.Commit(); err != nil {
		return currRev, errors.Join(err, rpctypes.ErrGRPCTooManyOps)
	}

	return maxRev, nil
}

func (t *transaction) Delete(ctx context.Context, key string, revision int64) (int64, *server.KeyValue, bool, error) {
	return t.backend.Delete(ctx, key, revision)
}

func (t *transaction) List(ctx context.Context, key, end string, limit, revision int64, keysOnly bool, labelSelector, fieldSelector string) (int64, []*server.KeyValue, error) {
	return t.backend.List(ctx, key, end, limit, revision, keysOnly, labelSelector, fieldSelector)
}

func (t *transaction) Count(ctx context.Context, key, end string, revision int64, labelSelector, fieldSelector string) (int64, int64, error) {
	return t.backend.Count(ctx, key, end, revision, labelSelector, fieldSelector)
}

func (t *transaction) Update(ctx context.Context, key string, value []byte, revision, lease int64) (int64, *server.KeyValue, bool, error) {
	return t.backend.Update(ctx, key, value, revision, lease)
}

func (t *transaction) Watch(ctx context.Context, key, end string, revision int64, labelSelector, fieldSelector string) server.WatchResult {
	return t.backend.Watch(ctx, key, end, revision, labelSelector, fieldSelector)
}

func (t *transaction) DbSize(ctx context.Context) (int64, error) {
	return t.backend.DbSize(ctx)
}

func (t *transaction) CurrentRevision(ctx context.Context) (int64, error) {
	return t.backend.CurrentRevision(ctx)
}

func (t *transaction) Compact(ctx context.Context, revision int64) (int64, error) {
	return t.backend.Compact(ctx, revision)
}

func (t *transaction) WaitForSyncTo(revision int64) {
	t.backend.WaitForSyncTo(revision)
}

func (t *transaction) Transaction(ctx context.Context) (server.Transaction, error) {
	return t.backend.Transaction(ctx)
}
