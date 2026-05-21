package natsbus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/nats-io/nats.go"

	"cyber-deploy-hub/internal/contracts"
)

type Inbox interface {
	Register(ctx context.Context, envelope contracts.Envelope) (bool, error)
	MarkProcessed(ctx context.Context, messageID string) error
	MarkFailed(ctx context.Context, messageID string, err error) error
}

type Handler func(ctx context.Context, envelope contracts.Envelope) error

type ConsumerOptions struct {
	Stream         string
	Subject        string
	Queue          string
	Durable        string
	MaxDeliver     int
	AckWait        time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type Consumer struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	inbox   Inbox
	handler Handler
	logger  *slog.Logger
	opts    ConsumerOptions
}

func NewConsumer(
	ctx context.Context,
	url string,
	name string,
	opts ConsumerOptions,
	inbox Inbox,
	handler Handler,
	logger *slog.Logger,
) (*Consumer, error) {
	if inbox == nil {
		return nil, errors.New("inbox is nil")
	}
	if handler == nil {
		return nil, errors.New("handler is nil")
	}
	opts = normalizeConsumerOptions(opts)

	conn, err := nats.Connect(
		url,
		nats.Name(name),
		nats.Timeout(5*time.Second),
		nats.ReconnectWait(500*time.Millisecond),
		nats.MaxReconnects(10),
	)
	if err != nil {
		return nil, err
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, err
	}

	bootstrap := &Publisher{conn: conn, js: js, logger: logger}
	if err := bootstrap.ensureStreams(ctx); err != nil {
		conn.Close()
		return nil, err
	}

	return &Consumer{
		conn:    conn,
		js:      js,
		inbox:   inbox,
		handler: handler,
		logger:  logger,
		opts:    opts,
	}, nil
}

func (c *Consumer) Start() error {
	subscribeOpts := []nats.SubOpt{
		nats.BindStream(c.opts.Stream),
		nats.Durable(c.opts.Durable),
		nats.ManualAck(),
		nats.AckWait(c.opts.AckWait),
		nats.MaxDeliver(c.opts.MaxDeliver),
	}

	var err error
	if c.opts.Queue != "" {
		c.sub, err = c.js.QueueSubscribe(c.opts.Subject, c.opts.Queue, c.handleMessage, subscribeOpts...)
	} else {
		c.sub, err = c.js.Subscribe(c.opts.Subject, c.handleMessage, subscribeOpts...)
	}
	return err
}

func (c *Consumer) Close() {
	if c.sub != nil {
		if err := c.sub.Drain(); err != nil && c.logger != nil {
			c.logger.Warn("failed to drain nats subscription", slog.Any("error", err))
		}
	}
	if c.conn != nil {
		if err := c.conn.Drain(); err != nil && c.logger != nil {
			c.logger.Warn("failed to drain nats connection", slog.Any("error", err))
		}
		c.conn.Close()
	}
}

func (c *Consumer) handleMessage(msg *nats.Msg) {
	ctx := context.Background()

	var envelope contracts.Envelope
	if err := json.Unmarshal(msg.Data, &envelope); err != nil {
		c.ackInvalidMessage(msg, err)
		return
	}

	registered, err := c.inbox.Register(ctx, envelope)
	if err != nil {
		c.nak(msg, 1, err, envelope)
		return
	}
	if !registered {
		c.ack(msg, envelope)
		return
	}

	if err := c.handler(ctx, envelope); err != nil {
		_ = c.inbox.MarkFailed(ctx, envelope.MessageID, err)
		delivery := deliveryAttemptInt(msg)
		if delivery >= c.opts.MaxDeliver {
			c.publishDLQ(ctx, envelope, err)
			c.ack(msg, envelope)
			return
		}
		c.nak(msg, delivery, err, envelope)
		return
	}

	if err := c.inbox.MarkProcessed(ctx, envelope.MessageID); err != nil {
		c.nak(msg, deliveryAttemptInt(msg), err, envelope)
		return
	}
	c.ack(msg, envelope)
}

func (c *Consumer) ackInvalidMessage(msg *nats.Msg, err error) {
	if c.logger != nil {
		c.logger.Error("failed to decode nats message", slog.Any("error", err))
	}
	if ackErr := msg.Ack(); ackErr != nil && c.logger != nil {
		c.logger.Error("failed to ack invalid nats message", slog.Any("error", ackErr))
	}
}

func (c *Consumer) ack(msg *nats.Msg, envelope contracts.Envelope) {
	if err := msg.Ack(); err != nil && c.logger != nil {
		attrs := append(contracts.LogAttrs(envelope), slog.Any("error", err))
		c.logger.LogAttrs(context.Background(), slog.LevelError, "failed to ack nats message", attrs...)
	}
}

func (c *Consumer) nak(msg *nats.Msg, attempt int, err error, envelope contracts.Envelope) {
	delay := backoffDelay(attempt, c.opts.InitialBackoff, c.opts.MaxBackoff)
	if nakErr := msg.NakWithDelay(delay); nakErr != nil && c.logger != nil {
		attrs := append(contracts.LogAttrs(envelope), slog.Any("error", nakErr))
		c.logger.LogAttrs(context.Background(), slog.LevelError, "failed to nak nats message", attrs...)
	}
	if c.logger != nil {
		attrs := append(contracts.LogAttrs(envelope), slog.Any("error", err), slog.Duration("retry_after", delay))
		c.logger.LogAttrs(context.Background(), slog.LevelWarn, "nats message handling failed", attrs...)
	}
}

func (c *Consumer) publishDLQ(ctx context.Context, envelope contracts.Envelope, handleErr error) {
	envelope.Error = &contracts.MessageError{
		Code:    "CONSUMER_HANDLER_FAILED",
		Message: truncateError(handleErr),
	}
	subject := contracts.Subject(envelope.MessageType).DLQ().String()
	payload, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	if _, err := c.js.Publish(subject, payload, nats.Context(ctx), nats.MsgId(envelope.MessageID+"-dlq")); err != nil && c.logger != nil {
		attrs := append(contracts.LogAttrs(envelope), slog.Any("error", err))
		c.logger.LogAttrs(ctx, slog.LevelError, "failed to publish nats message to dlq", attrs...)
	}
}

func normalizeConsumerOptions(opts ConsumerOptions) ConsumerOptions {
	if opts.Subject == "" {
		opts.Subject = "cmd.>"
	}
	if opts.Stream == "" {
		if len(opts.Subject) >= 4 && opts.Subject[:4] == "evt." {
			opts.Stream = "EVENTS"
		} else {
			opts.Stream = "COMMANDS"
		}
	}
	if opts.Durable == "" {
		opts.Durable = "default"
	}
	if opts.MaxDeliver <= 0 {
		opts.MaxDeliver = 5
	}
	if opts.AckWait <= 0 {
		opts.AckWait = 30 * time.Second
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = time.Minute
	}
	return opts
}

func deliveryAttempt(msg *nats.Msg) uint64 {
	metadata, err := msg.Metadata()
	if err != nil {
		return 1
	}
	if metadata.NumDelivered == 0 {
		return 1
	}
	return metadata.NumDelivered
}

func deliveryAttemptInt(msg *nats.Msg) int {
	attempt := deliveryAttempt(msg)
	if attempt > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(attempt)
}

func backoffDelay(attempt int, initial time.Duration, max time.Duration) time.Duration {
	if attempt <= 1 {
		return initial
	}
	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	return delay
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) <= 1024 {
		return text
	}
	return text[:1024]
}
