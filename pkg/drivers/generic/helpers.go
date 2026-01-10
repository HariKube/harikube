package generic

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/k3s-io/kine/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/selection"
)

var (
	decoder             = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
	unstructuredDecoder = json.NewSerializerWithOptions(
		json.DefaultMetaFactory,
		unstructuredscheme.NewUnstructuredCreator(),
		unstructuredscheme.NewUnstructuredObjectTyper(),
		json.SerializerOptions{Yaml: true, Pretty: false, Strict: false},
	)

	labelsSelect = ` AND %s IN (
		SELECT kine_id
		FROM kine_labels
		WHERE kine_name LIKE ? AND (%s)
		GROUP BY kine_id
		HAVING COUNT(kine_id) = ?
	)
	`

	fieldsSelect = ` AND %s IN (
		SELECT kine_id
		FROM kine_fields
		WHERE kine_name LIKE ? AND (%s)
		GROUP BY kine_id
	)
	`

	paramsRegex = regexp.MustCompile(`\?`)
)

func decodeObject(key string, value []byte) (runtime.Object, map[string]string, fields.Set, []metav1.OwnerReference, error) {
	obj := util.GetObjectByKey(key)
	if _, _, err := decoder.Decode(value, nil, obj); err != nil {
		return nil, nil, nil, nil, err
	}

	return obj, util.GetLabelsSetByObject(obj), util.GetFieldsSetByObject(obj, value), util.GetOwnersByObject(obj), nil
}

func renderSelectorsWhere(sql, prefix, labelSelector, fieldSelector string, args []any, selectorLookupSQL string) (string, []any, error) {
	var ID string
	switch {
	case strings.Contains(sql, "maxkv.theid"):
		ID = "maxkv.theid"
	case strings.Contains(sql, "c.theid"):
		ID = "c.theid"
	default:
		ID = "kv.id"
	}

	numbered := strings.Contains(sql, "$")

	labelsWhere, args, err := renderLabelSelectorWhere(ID, prefix, labelSelector, args, numbered)
	if err != nil {
		return "", args, err
	}

	fieldsWhere, args, err := renderFieldSelectorWhere(ID, prefix, fieldSelector, args, numbered, selectorLookupSQL)
	if err != nil {
		return "", args, err
	}

	return labelsWhere + fieldsWhere, args, nil
}

func renderLabelSelectorWhere(ID, prefix, labelSelector string, args []any, numbered bool) (string, []any, error) {
	if labelSelector == "" {
		return "", args, nil
	}

	selector, err := labels.Parse(labelSelector)
	if err != nil {
		return "", args, err
	}

	reqs, selectable := selector.Requirements()
	if !selectable {
		return "", args, nil
	}

	inGen := func(l int) string {
		return strings.TrimSuffix(strings.Repeat("?,", l), ",")
	}

	argsN := len(args)

	args = append(args, prefix)

	wheres := []string{}
	for _, req := range reqs {
		valueList := req.Values().List()
		switch req.Operator() {
		case selection.DoesNotExist:
			wheres = append(wheres, "(kine_id NOT IN (SELECT kine_id FROM kine_labels WHERE kine_name LIKE ? AND name = ? GROUP BY kine_id))")
			args = append(args, prefix, req.Key())
		case selection.Equals:
			fallthrough
		case selection.DoubleEquals:
			wheres = append(wheres, "(name = ? AND value = ?)")
			args = append(args, req.Key(), valueList[0])
		case selection.In:
			wheres = append(wheres, "(name = ? AND value IN ("+inGen(len(valueList))+"))")
			args = append(args, req.Key())
			for _, v := range valueList {
				args = append(args, v)
			}
		case selection.NotEquals:
			wheres = append(wheres, "(name = ? AND value != ?)")
			args = append(args, req.Key(), valueList[0])
		case selection.NotIn:
			wheres = append(wheres, "(name = ? AND value NOT IN ("+inGen(len(valueList))+"))")
			args = append(args, req.Key())
			for _, v := range valueList {
				args = append(args, v)
			}
		case selection.Exists:
			wheres = append(wheres, "(name IN (?))")
			args = append(args, req.Key())
		case selection.GreaterThan:
			wheres = append(wheres, "(name = ? AND value > ?)")
			args = append(args, req.Key(), valueList[0])
		case selection.LessThan:
			wheres = append(wheres, "(name = ? AND value < ?)")
			args = append(args, req.Key(), valueList[0])
		}
	}

	args = append(args, len(reqs))

	where := fmt.Sprintf(labelsSelect, ID, strings.Join(wheres, " OR "))
	if numbered {
		where = replaceParamsToNumbers(where, argsN)
	}

	return where, args, nil
}

func renderFieldSelectorWhere(ID, prefix, fieldSelector string, args []any, numbered bool, selectorLookupSQL string) (string, []any, error) {
	if fieldSelector == "" {
		return "", args, nil
	}

	selector, err := fields.ParseSelector(fieldSelector)
	if err != nil {
		return "", args, err
	}

	argsN := len(args)

	args = append(args, prefix)

	wheres := []string{}
	for _, req := range selector.Requirements() {
		req.Field = strings.ReplaceAll(req.Field, ".", "_")

		sl := selectorLookupSQL
		if strings.Contains(sl, "%s") {
			sl = fmt.Sprintf(sl, req.Field)
		} else {
			args = append(args, req.Field)
		}

		args = append(args, req.Value)

		switch req.Operator {
		case selection.Equals:
			fallthrough
		case selection.DoubleEquals:
			wheres = append(wheres, "("+sl+")")
		case selection.NotEquals:
			wheres = append(wheres, "(NOT "+sl+")")
		}
	}

	where := fmt.Sprintf(fieldsSelect, ID, strings.Join(wheres, " AND "))
	if numbered {
		where = replaceParamsToNumbers(where, argsN)
	}

	return where, args, nil
}

func replaceParamsToNumbers(where string, args int) string {
	pref := "$"
	return paramsRegex.ReplaceAllStringFunc(where, func(string) string {
		args++
		return pref + strconv.Itoa(args)
	})
}
