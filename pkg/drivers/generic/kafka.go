package generic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/sirupsen/logrus"
)

const brokerAddress = "172.17.0.1:9092"

func StarKafkaConsumer(ctx context.Context, wg *sync.WaitGroup, backend *Generic) {
	numWorkers := 3
	topic := "incoming"
	groupID := "stream-reader-group"

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			r := kafka.NewReader(kafka.ReaderConfig{
				Brokers:        []string{brokerAddress},
				Topic:          topic,
				GroupID:        groupID,
				MinBytes:       10e3,
				MaxBytes:       16e6,
				CommitInterval: time.Second,
				StartOffset:    kafka.FirstOffset,
			})

			logrus.Infof("Worker %d subscribed to topic: %s", workerID, topic)

			for {
				if ctx.Err() != nil {
					return
				}

				m, err := r.FetchMessage(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					logrus.Errorf("Worker %d failed to fetch message: %v", workerID, err)

					break
				}

				fmt.Printf("Worker %d: Topic = %s, Partition = %d, Offset = %d, Key = %s \n",
					workerID, m.Topic, m.Partition, m.Offset, string(m.Key))

				keyParts := strings.Split(m.Topic, "#")
				if len(keyParts) != 2 {
					logrus.Errorf("Worker %d received invalid topic: %v", errors.New("invalid topic format"))

					continue
				}

				opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				switch keyParts[1] {
				case "create":
					if _, err := backend.Insert(opCtx, keyParts[0], true, false, 0, 0, 0, m.Value, nil); err != nil {
						logrus.Errorf("Worker %d failed to create object: %v", workerID, err)
					}
				case "update":
					if _, err := backend.Insert(opCtx, keyParts[0], false, false, 0, 0, 0, m.Value, nil); err != nil {
						logrus.Errorf("Worker %d failed to update object: %v", workerID, err)
					}
				case "delete":
					if _, err := backend.Insert(opCtx, keyParts[0], false, true, 0, 0, 0, m.Value, nil); err != nil {
						logrus.Errorf("Worker %d failed to delete object: %v", workerID, err)
					}
				}
				cancel()

				if err := r.CommitMessages(ctx, m); err != nil {
					logrus.Errorf("Worker %d error committing message: %v", workerID, err)
				}

				if ctx.Err() != nil {
					return
				}
			}

			r.Close()

			time.Sleep(time.Second)
		}(i)
	}
}

type KafkaProducer struct {
	writer *kafka.Writer
}

func NewKafkaProducer() *KafkaProducer {
	return &KafkaProducer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokerAddress),
			Topic:        "outgoing",
			Balancer:     &kafka.LeastBytes{},
			BatchTimeout: 10 * time.Millisecond,
			RequiredAcks: kafka.RequireAll,
		},
	}
}

func (p *KafkaProducer) SendMessage(ctx context.Context, key string, value []byte) error {
	err := p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: value,
	})
	if err != nil {
		logrus.Errorf("Failed to write message to Kafka (key: %s): %v", key, err)

		return err
	}
	return nil
}

func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
