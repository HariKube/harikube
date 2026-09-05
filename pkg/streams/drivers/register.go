package drivers

import "github.com/k3s-io/kine/pkg/server"

var steramProducers = map[string][]MessageProducer{}

type MessageProducer interface {
	SendMessage(string, []byte, func(error), map[string][]byte)
}

func RegisterProducer(producer string, event MessageProducer) {
	if _, ok := steramProducers[producer]; !ok {
		steramProducers[producer] = []MessageProducer{}
	}

	steramProducers[producer] = append(steramProducers[producer], event)
}

func GetProducers(producer string) []MessageProducer {
	return steramProducers[producer]
}

func SendMessage(producer string, events server.Events) {
	for e := range events {
		for i := range steramProducers[producer] {
			if events[e].KV != nil {
				steramProducers[producer][i].SendMessage(events[e].KV.Key, events[e].KV.Value, nil, nil)
			} else if events[e].PrevKV != nil {
				steramProducers[producer][i].SendMessage(events[e].PrevKV.Key, nil, nil, nil)
			}
		}
	}
}
