package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/a-gierczak/paratrooper/internal/logger"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const (
	streamName               = "UPDATES"
	updateSubjectsWildcard   = "UPDATE.>"
	processUpdateSubjectName = "UPDATE.PROCESS"
)

type Connection struct {
	nc                   *nats.Conn
	js                   jetstream.JetStream
	stream               jetstream.Stream
	dlqSub               *nats.Subscription
	processUpdateCons    jetstream.Consumer
	processUpdateConsCtx jetstream.ConsumeContext
}

func (c *Connection) connect(uri string) error {
	conn, err := nats.Connect(uri)
	if err != nil {
		return fmt.Errorf("failed to connect to nats: %w", err)
	}

	c.nc = conn

	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("failed to create jetstream: %w", err)
	}
	c.js = js

	cfg := jetstream.StreamConfig{
		Name:      streamName,
		Retention: jetstream.WorkQueuePolicy,
		Subjects:  []string{updateSubjectsWildcard},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	c.stream = stream

	return nil
}

func Connect(ctx context.Context, uri string) (*Connection, error) {
	log := logger.FromContext(ctx)
	conn := new(Connection)

	err := conn.connect(uri)
	if err != nil {
		return nil, err
	}

	log.Info("connected to NATS")
	return conn, nil
}

func (c *Connection) Consume(
	ctx context.Context,
	msgHandler jetstream.MessageHandler,
	dlqHandler func(msg *jetstream.RawStreamMsg),
) error {
	log := logger.FromContext(ctx)

	streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	consumerName := "process-update"
	cons, err := c.js.CreateOrUpdateConsumer(
		streamCtx,
		streamName,
		jetstream.ConsumerConfig{
			AckPolicy:     jetstream.AckExplicitPolicy,
			Name:          consumerName,
			Durable:       consumerName,
			FilterSubject: processUpdateSubjectName,
			MaxDeliver:    5,
			BackOff: []time.Duration{
				5 * time.Second,
				12 * time.Second,
				19 * time.Second,
				30 * time.Second,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}
	c.processUpdateCons = cons
	log.Info("process update consumer created")

	consumeCtx, err := c.processUpdateCons.Consume(msgHandler, jetstream.PullMaxMessages(1))
	if err != nil {
		return fmt.Errorf("failed to consume messages: %w", err)
	}
	c.processUpdateConsCtx = consumeCtx

	dlqEventSubject := fmt.Sprintf("$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.%s.>", streamName)
	dlqSub, err := c.nc.Subscribe(dlqEventSubject, c.maxDeliveriesHandlerWrapper(ctx, dlqHandler))
	if err != nil {
		return fmt.Errorf("failed to subscribe to max deliveries dlq: %w", err)
	}
	c.dlqSub = dlqSub
	log.Info("subscribed to max deliveries dlq")

	return nil
}

func (c *Connection) maxDeliveriesHandlerWrapper(
	ctx context.Context,
	handler func(msg *jetstream.RawStreamMsg),
) func(msg *nats.Msg) {
	log := logger.FromContext(ctx)
	log = log.With(zap.String("consumer", "dlq"))
	return func(msg *nats.Msg) {
		type DLQMessage struct {
			StreamSeq *int `json:"stream_seq,omitempty"`
		}

		var dlqMsg DLQMessage
		err := json.Unmarshal(msg.Data, &dlqMsg)
		if err != nil {
			log.Error("failed to unmarshal dlq message", zap.Error(err))
			return
		}

		if dlqMsg.StreamSeq == nil {
			log.Error("stream_seq is not set")
			return
		}

		streamSeq := uint64(*dlqMsg.StreamSeq)
		rawMsg, err := c.stream.GetMsg(ctx, streamSeq)
		if err != nil {
			log.Error("failed to get message from stream", zap.Error(err))
			return
		}

		handler(rawMsg)

		if err := c.stream.DeleteMsg(ctx, streamSeq); err != nil {
			log.Error(
				"failed to delete message from stream",
				zap.Error(err),
				zap.Uint64("stream_seq", streamSeq),
			)
		} else {
			log.Info("deleted message from stream", zap.Uint64("stream_seq", streamSeq))
		}
	}
}

func (c *Connection) PopOriginalMessage(
	ctx context.Context,
	msg *nats.Msg,
) ([]byte, error) {
	type DLQMessage struct {
		StreamSeq *int `json:"stream_seq,omitempty"`
	}

	var dlqMsg DLQMessage
	err := json.Unmarshal(msg.Data, &dlqMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal dlq message: %w", err)
	}

	if dlqMsg.StreamSeq == nil {
		return nil, fmt.Errorf("stream_seq is not set")
	}

	streamSeq := uint64(*dlqMsg.StreamSeq)
	rawMsg, err := c.stream.GetMsg(ctx, streamSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to get message from stream: %w", err)
	}

	log := logger.FromContext(ctx)
	if err := c.stream.DeleteMsg(ctx, streamSeq); err != nil {
		log.Error(
			"failed to delete message from stream",
			zap.Error(err),
			zap.Uint64("stream_seq", streamSeq),
		)
	} else {
		log.Info("deleted message from stream", zap.Uint64("stream_seq", streamSeq))
	}

	return rawMsg.Data, nil
}

func (c *Connection) Close() {
	if c.dlqSub != nil {
		c.dlqSub.Unsubscribe()
	}
	if c.processUpdateConsCtx != nil {
		c.processUpdateConsCtx.Stop()
	}
	c.nc.Close()
}

func (c *Connection) HealthCheck() error {
	natsServerURLs := c.nc.Servers()
	if len(natsServerURLs) == 0 {
		return nats.ErrNoServers
	}

	type serverHealth struct {
		Status string `json:"status"`
	}

	for _, serverURL := range natsServerURLs {
		parsedURL, err := url.Parse(serverURL)
		if err != nil {
			return fmt.Errorf("url.Parse: %w", err)
		}

		healthCheckURL := fmt.Sprintf(
			"http://%s:8222/healthz?js-enabled-only=true",
			parsedURL.Hostname(),
		)
		resp, err := http.Get(healthCheckURL)
		if err != nil {
			return fmt.Errorf("failed to get NATS health: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s returned %d status code", healthCheckURL, resp.StatusCode)
		}

		var health serverHealth
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return fmt.Errorf("failed to decode NATS health response: %w", err)
		}

		if health.Status != "ok" {
			return fmt.Errorf(
				"NATS server at %s returned non-ok status: %s",
				healthCheckURL,
				health.Status,
			)
		}
	}

	return nil
}
