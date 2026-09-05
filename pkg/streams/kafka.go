package streams

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/k3s-io/kine/pkg/server"
	"github.com/k3s-io/kine/pkg/util"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	types "k8s.io/apimachinery/pkg/types"
)

var (
	decoder = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()

	unstructuredDecoder = kjson.NewSerializerWithOptions(
		kjson.DefaultMetaFactory,
		unstructuredscheme.NewUnstructuredCreator(),
		unstructuredscheme.NewUnstructuredObjectTyper(),
		kjson.SerializerOptions{Yaml: true, Pretty: false, Strict: false},
	)
)

type ReaderConfig struct {
	Brokers     []string `json:"brokers"`
	GroupID     string   `json:"group_id"`
	GroupTopics []string `json:"group_topics"`
	Topic       string   `json:"topic"`
	Partition   int      `json:"partition"`
	// Dialer                 *Dialer       `json:"dialer"`
	QueueCapacity          int   `json:"queue_capacity"`
	MinBytes               int   `json:"min_bytes"`
	MaxBytes               int   `json:"max_bytes"`
	MaxWait                int64 `json:"max_wait"`
	ReadBatchTimeout       int64 `json:"read_batch_timeout"`
	ReadLagInterval        int64 `json:"read_lag_interval"`
	HeartbeatInterval      int64 `json:"heartbeat_interval"`
	CommitInterval         int64 `json:"commit_interval"`
	PartitionWatchInterval int64 `json:"partition_watch_interval"`
	WatchPartitionChanges  bool  `json:"watch_partition_changes"`
	SessionTimeout         int64 `json:"session_timeout"`
	RebalanceTimeout       int64 `json:"rebalance_timeout"`
	JoinGroupBackoff       int64 `json:"join_group_backoff"`
	RetentionTime          int64 `json:"retention_time"`
	StartOffset            int64 `json:"start_offset"`
	ReadBackoffMin         int64 `json:"read_backoff_min"`
	ReadBackoffMax         int64 `json:"read_backoff_max"`
	// Logger                 Logger         `json:"logger"`
	// ErrorLogger            Logger         `json:"error_logger"`
	IsolationLevel        int8 `json:"isolation_level"`
	MaxAttempts           int  `json:"max_attempts"`
	OffsetOutOfRangeError bool `json:"offset_out_of_range_error"`
}

type KafkaWriter struct {
	Addr            string `json:"addr"`
	Topic           string `json:"topic"`
	MaxAttempts     int    `json:"max_attempts"`
	WriteBackoffMin int64  `json:"write_backoff_min"`
	WriteBackoffMax int64  `json:"write_backoff_max"`
	BatchSize       int    `json:"batch_size"`
	BatchBytes      int64  `json:"batch_bytes"`
	BatchTimeout    int64  `json:"batch_timeout"`
	ReadTimeout     int64  `json:"read_timeout"`
	WriteTimeout    int64  `json:"write_timeout"`
	RequiredAcks    int    `json:"required_acks"`
	Async           bool   `json:"async"`
	Compression     int8   `json:"compression"`
	// Logger                 Logger                              `json:"-"`
	// ErrorLogger            Logger                              `json:"-"`
	// Transport              RoundTripper `json:"-"`
	AllowAutoTopicCreation bool `json:"allow_auto_topic_creation"`
}

func StartKafkaConsumer(ctx context.Context, wg *sync.WaitGroup, backend server.Backend, configEnc string, dlqConfigEnc string) error {
	configBytes, err := base64.StdEncoding.DecodeString(configEnc)
	if err != nil {
		return err
	}

	config := ReaderConfig{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return err
	}

	var dlqWriter *KafkaProducer
	if dlqConfigEnc != "" {
		dlqWriter, err = NewKafkaProducer(ctx, dlqConfigEnc)
		if err != nil {
			return err
		}
	}

	wg.Add(1)
	go func() {
		if dlqWriter != nil {
			defer dlqWriter.Close()
		}
		defer wg.Done()

		for {
			r := kafka.NewReader(kafka.ReaderConfig{
				Brokers:                config.Brokers,
				GroupID:                config.GroupID,
				GroupTopics:            config.GroupTopics,
				Topic:                  config.Topic,
				Partition:              config.Partition,
				QueueCapacity:          config.QueueCapacity,
				MinBytes:               10e3,
				MaxBytes:               16e6,
				MaxWait:                time.Duration(config.MaxWait),
				ReadBatchTimeout:       time.Duration(config.ReadBatchTimeout),
				ReadLagInterval:        time.Duration(config.ReadLagInterval),
				HeartbeatInterval:      time.Duration(config.HeartbeatInterval),
				CommitInterval:         time.Duration(config.CommitInterval),
				PartitionWatchInterval: time.Duration(config.PartitionWatchInterval),
				WatchPartitionChanges:  config.WatchPartitionChanges,
				SessionTimeout:         time.Duration(config.SessionTimeout),
				RebalanceTimeout:       time.Duration(config.RebalanceTimeout),
				JoinGroupBackoff:       time.Duration(config.JoinGroupBackoff),
				RetentionTime:          time.Duration(config.RetentionTime),
				StartOffset:            config.StartOffset,
				ReadBackoffMin:         time.Duration(config.ReadBackoffMin),
				ReadBackoffMax:         time.Duration(config.ReadBackoffMax),
				MaxAttempts:            config.MaxAttempts,
				OffsetOutOfRangeError:  config.OffsetOutOfRangeError,
			})

			brokers := fmt.Sprintf("%v", config.Brokers)
			logrus.Infof("Worker %s subscribed to topic: %s", brokers, config.Topic)

			for {
				if ctx.Err() != nil {
					break
				}

				m, err := r.FetchMessage(ctx)
				if err != nil {
					if ctx.Err() != nil {
						break
					}

					logrus.Errorf("Worker %s failed to fetch message: %v", brokers, err)

					continue
				}

				key := string(m.Key)

				fmt.Printf("Worker %ss: Topic = %s, Partition = %d, Offset = %d, Key = %s \n",
					brokers, m.Topic, m.Partition, m.Offset, key)

				keyParts := strings.Split(key, "#")
				if len(keyParts) != 2 {
					logrus.Errorf("Worker %s received invalid key for %s: %v", brokers, key, errors.New("invalid topic format"))

					if dlqWriter != nil {
						br := false
						dlqWriter.SendMessage(key, m.Value,
							func(dlqErr error) {
								logrus.Errorf("Worker %s failed to write to DLQ [Key: %s]: %v. Skipping offset commit.", brokers, key, dlqErr)

								br = true
							},
							map[string][]byte{
								"x-error-reason": []byte(fmt.Sprintf("invalid key format: %s", key)),
							},
						)
						if br {
							break
						}
					}

					commCtx, commCancel := context.WithTimeout(ctx, 5*time.Second)

					// TODO retry logic
					if err := r.CommitMessages(commCtx, m); err != nil {
						logrus.Errorf("Worker %s error committing message [%s]: %v", brokers, key, err)

						commCancel()

						break
					}

					commCancel()

					continue
				}

				var backendErr error
				opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
				switch keyParts[1] {
				case "create":
					obj := &unstructured.Unstructured{}
					if _, _, backendErr = unstructuredDecoder.Decode(m.Value, nil, obj); backendErr != nil {
						logrus.Errorf("Worker %s failed to decode object [%s]: %v", brokers, key, backendErr)

						break
					}

					if obj.GetCreationTimestamp().Time.IsZero() {
						obj.SetCreationTimestamp(metav1.NewTime(time.Now()))
					}
					if obj.GetGeneration() == 0 {
						obj.SetGeneration(1)
					}
					if obj.GetUID() == "" {
						obj.SetUID(types.UID(uuid.New().String()))
					}
					if obj.GetNamespace() == "" {
						obj.SetNamespace("default")
					}

					var objNewValue []byte
					objNewValue, backendErr = jsoniter.Marshal(obj)
					if backendErr != nil {
						logrus.Errorf("Worker %s failed to default new object [%s]: %v", brokers, key, backendErr)

						break
					}

					if _, backendErr = backend.Create(opCtx, keyParts[0], objNewValue, 0); backendErr != nil {
						logrus.Errorf("Worker %s failed to create object [%s]: %v", brokers, key, backendErr)
					}
				case "update":
					obj := util.GetObjectByKey(keyParts[0])
					if _, _, backendErr = decoder.Decode(m.Value, nil, obj); backendErr != nil {
						logrus.Errorf("Worker %s failed to decode object [%s]: %v", brokers, key, backendErr)

						break
					}

					var revision int64
					resourceVersion := util.GetResourceVersionByObject(obj)
					revision, backendErr = strconv.ParseInt(resourceVersion, 10, 64)
					if backendErr != nil {
						logrus.Errorf("Worker %s failed to parse resource version %s of [%s]: %v", brokers, resourceVersion, key, backendErr)

						break
					}

					if _, _, _, backendErr = backend.Update(opCtx, keyParts[0], m.Value, revision, 0); backendErr != nil {
						logrus.Errorf("Worker %s failed to update object [%s]: %v", brokers, key, backendErr)
					}
				case "delete":
					if _, _, _, backendErr = backend.Delete(opCtx, keyParts[0], 0); backendErr != nil {
						logrus.Errorf("Worker %s failed to delete object [%s]: %v", brokers, key, backendErr)
					}
				default:
					backendErr = fmt.Errorf("Worker %s unsupported operation: %s", brokers, keyParts[1])
				}
				opCancel()

				commCtx, commCancel := context.WithTimeout(ctx, 10*time.Second)

				if backendErr != nil && dlqWriter != nil {
					br := false
					dlqWriter.SendMessage(key, m.Value,
						func(dlqErr error) {
							logrus.Errorf("Worker %s failed to write to DLQ [Key: %s]: %v. Skipping offset commit.", brokers, key, dlqErr)

							br = true
						},
						map[string][]byte{
							"x-error-reason": []byte(backendErr.Error()),
						},
					)
					if br {
						commCancel()

						break
					}
				}

				// TODO retry logic
				if err := r.CommitMessages(commCtx, m); err != nil {
					logrus.Errorf("Worker %s error committing message [%s]: %v", brokers, key, err)

					commCancel()

					break
				}

				commCancel()

				if ctx.Err() != nil {
					break
				}
			}

			if err := r.Close(); err != nil {
				logrus.Errorf("Worker %s failed to close reader: %v", brokers, err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				logrus.Infof("Worker %s cooldown finished: %s", brokers, config.Topic)
			}
		}
	}()

	return nil
}

type KafkaProducer struct {
	name   string
	ctx    context.Context
	writer *kafka.Writer
}

func NewKafkaProducer(ctx context.Context, configEnc string) (*KafkaProducer, error) {
	configBytes, err := base64.StdEncoding.DecodeString(configEnc)
	if err != nil {
		return nil, err
	}

	config := KafkaWriter{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, err
	}

	return &KafkaProducer{
		name: fmt.Sprintf("%s", config.Addr),
		ctx:  ctx,
		writer: &kafka.Writer{
			Addr:                   kafka.TCP(config.Addr),
			Topic:                  config.Topic,
			Balancer:               &kafka.LeastBytes{},
			MaxAttempts:            config.MaxAttempts,
			WriteBackoffMin:        time.Duration(config.WriteBackoffMin),
			WriteBackoffMax:        time.Duration(config.WriteBackoffMax),
			BatchSize:              config.BatchSize,
			BatchBytes:             config.BatchBytes,
			BatchTimeout:           time.Duration(config.BatchTimeout),
			ReadTimeout:            time.Duration(config.ReadTimeout),
			WriteTimeout:           time.Duration(config.WriteTimeout),
			RequiredAcks:           kafka.RequiredAcks(config.RequiredAcks),
			Async:                  config.Async,
			Compression:            compress.Compression(config.Compression),
			AllowAutoTopicCreation: config.AllowAutoTopicCreation,
		},
	}, nil
}

func (p *KafkaProducer) SendMessage(key string, value []byte, errorCallback func(error), headers map[string][]byte) {
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	msg := kafka.Message{
		Key:   []byte(key),
		Value: value,
	}
	if len(headers) != 0 {
		msg.Headers = []kafka.Header{}
		for k, v := range headers {
			msg.Headers = append(msg.Headers, kafka.Header{
				Key:   k,
				Value: v,
			})
		}
	}

	// TODO retry logic
	err := p.writer.WriteMessages(ctx, msg)
	if err != nil {
		logrus.Errorf("Failed to write message to Kafka %s (key: %s): %v", p.name, key, err)

		if errorCallback != nil {
			errorCallback(err)
		}
	}
}

func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
