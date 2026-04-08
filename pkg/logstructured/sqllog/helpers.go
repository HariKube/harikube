package sqllog

import (
	"strings"
	"time"

	cache "github.com/Code-Hex/go-generics-cache"
	"github.com/k3s-io/kine/pkg/server"
	"github.com/k3s-io/kine/pkg/util"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
)

var (
	labelSelectorCache = cache.New(cache.AsFIFO[string, labels.Selector]())
	fieldSelectorCache = cache.New(cache.AsFIFO[string, fields.Selector]())

	decoder = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
)

func filterEventBySelectors(event *server.Event, labelSelector, fieldSelector string) bool {
	if event.KV == nil || len(event.KV.Value) == 0 {
		return true
	} else if labelSelector == "" && fieldSelector == "" {
		return true
	}

	obj := util.GetObjectByKey(event.KV.Key)
	if _, _, err := decoder.Decode(event.KV.Value, nil, obj); err != nil {
		return true
	}

	labelsMatch := true
	if labelSelector != "" {
		ls, _ := labelSelectorCache.GetOrSet(labelSelector, func() labels.Selector {
			ls, err := labels.Parse(labelSelector)
			if err != nil {
				return labels.Everything()
			}

			return ls
		}(), cache.WithExpiration(time.Hour))

		if ls != nil && !ls.Empty() {
			labelsMatch = ls.Matches(util.GetLabelsSetByObject(obj))
		}
	}

	fieldsMatch := true
	if fieldSelector != "" {
		fs, _ := fieldSelectorCache.GetOrSet(fieldSelector, func() fields.Selector {
			fs, err := fields.ParseSelector(fieldSelector)
			if err != nil {
				return fields.Everything()
			}

			return fs
		}(), cache.WithExpiration(time.Hour))

		if fs != nil && !fs.Empty() {
			fields := util.GetFieldsSetByObject(obj, event.KV.Value)

			matches := 0
			for _, req := range fs.Requirements() {
				value := fields[req.Field]
				switch req.Operator {
				case selection.Equals:
					fallthrough
				case selection.DoubleEquals:
					if strings.Contains(value, req.Value) {
						matches++
					}
				case selection.NotEquals:
					if !strings.Contains(value, req.Value) {
						matches++
					}
				}
			}

			fieldsMatch = len(fs.Requirements()) == matches
		}
	}

	return labelsMatch && fieldsMatch
}
