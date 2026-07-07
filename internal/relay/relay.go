package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store"
)

type Publisher interface {
	Publish(topic, key string, value []byte) error
	Close() error
}

type saramaPublisher struct {
	producer sarama.SyncProducer
}

func NewSaramaPublisher(brokers []string) (Publisher, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3
	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}
	return &saramaPublisher{producer: producer}, nil
}

func (p *saramaPublisher) Publish(topic, key string, value []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(value),
	}
	if _, _, err := p.producer.SendMessage(msg); err != nil {
		return fmt.Errorf("send kafka message: %w", err)
	}
	return nil
}

func (p *saramaPublisher) Close() error {
	return p.producer.Close()
}

type Relay struct {
	store     store.Store
	topic     string
	source    string
	connect   func() (Publisher, error)
	publisher Publisher
	interval  time.Duration
	batchSize int
	log       *zap.Logger
	now       func() time.Time
}

func New(st store.Store, topic string, connect func() (Publisher, error), log *zap.Logger) *Relay {
	return &Relay{
		store:     st,
		topic:     topic,
		source:    "pomodoro",
		connect:   connect,
		interval:  2 * time.Second,
		batchSize: 100,
		log:       log,
		now:       time.Now,
	}
}

func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	defer r.closePublisher()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Flush(ctx); err != nil {
				r.log.Debug("outbox flush failed", zap.Error(err))
			}
		}
	}
}

func (r *Relay) Flush(ctx context.Context) error {
	events, err := r.store.Outbox().Unpublished(ctx, r.batchSize)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	if r.publisher == nil {
		pub, err := r.connect()
		if err != nil {
			return fmt.Errorf("connect publisher: %w", err)
		}
		r.publisher = pub
	}
	for i := range events {
		e := events[i]
		value, err := Envelope(r.source, e)
		if err != nil {
			if markErr := r.store.Outbox().MarkFailed(ctx, e.ID, err.Error()); markErr != nil {
				return markErr
			}
			r.log.Warn("skip malformed outbox event",
				zap.String("event_id", e.ID.String()),
				zap.String("event_type", e.EventType),
				zap.Error(err))
			continue
		}
		if err := r.publisher.Publish(r.topic, e.AggregateID.String(), value); err != nil {
			if markErr := r.store.Outbox().MarkFailed(ctx, e.ID, err.Error()); markErr != nil {
				return markErr
			}
			r.closePublisher()
			return err
		}
		if err := r.store.Outbox().MarkPublished(ctx, e.ID, r.now()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Relay) closePublisher() {
	if r.publisher != nil {
		_ = r.publisher.Close()
		r.publisher = nil
	}
}

func Envelope(source string, e models.OutboxEvent) ([]byte, error) {
	envelope := map[string]any{
		"specversion":     "1.0",
		"id":              e.ID.String(),
		"source":          source,
		"type":            e.EventType,
		"subject":         e.AggregateID.String(),
		"time":            e.CreatedAt.UTC().Format(time.RFC3339),
		"datacontenttype": "application/json",
		"data":            e.Payload,
	}
	value, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal cloudevent: %w", err)
	}
	return value, nil
}
