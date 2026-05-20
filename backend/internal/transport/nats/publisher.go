package natsbus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"cyber-deploy-hub/internal/contracts"
)

type Publisher struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	logger *slog.Logger
}

func NewPublisher(ctx context.Context, url string, name string, logger *slog.Logger) (*Publisher, error) {
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

	publisher := &Publisher{conn: conn, js: js, logger: logger}
	if err := publisher.ensureStreams(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return publisher, nil
}

func (p *Publisher) Publish(ctx context.Context, subject string, envelope contracts.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = p.js.Publish(
		subject,
		payload,
		nats.Context(ctx),
		nats.MsgId(envelope.MessageID),
	)
	return err
}

func (p *Publisher) Ping(ctx context.Context) error {
	if p.conn == nil || p.conn.Status() != nats.CONNECTED {
		return errors.New("nats is not connected")
	}
	return p.conn.FlushWithContext(ctx)
}

func (p *Publisher) Close() {
	if p.conn != nil {
		if err := p.conn.Drain(); err != nil && p.logger != nil {
			p.logger.Warn("failed to drain nats connection", slog.Any("error", err))
		}
		p.conn.Close()
	}
}

func (p *Publisher) ensureStreams(ctx context.Context) error {
	streams := []*nats.StreamConfig{
		{
			Name:      "COMMANDS",
			Subjects:  []string{"cmd.>"},
			Retention: nats.LimitsPolicy,
			Storage:   nats.FileStorage,
			Replicas:  1,
		},
		{
			Name:      "EVENTS",
			Subjects:  []string{"evt.>"},
			Retention: nats.LimitsPolicy,
			Storage:   nats.FileStorage,
			Replicas:  1,
		},
		{
			Name:      "DLQ",
			Subjects:  []string{"dlq.>"},
			Retention: nats.LimitsPolicy,
			Storage:   nats.FileStorage,
			Replicas:  1,
		},
	}

	for _, stream := range streams {
		if _, err := p.js.StreamInfo(stream.Name, nats.Context(ctx)); err == nil {
			continue
		} else if !isStreamNotFound(err) {
			return err
		}

		if _, err := p.js.AddStream(stream, nats.Context(ctx)); err != nil && !strings.Contains(err.Error(), "stream name already in use") {
			return err
		}
		if p.logger != nil {
			p.logger.Info("nats stream ensured", slog.String("stream", stream.Name))
		}
	}
	return nil
}

func isStreamNotFound(err error) bool {
	return errors.Is(err, nats.ErrStreamNotFound) || strings.Contains(strings.ToLower(err.Error()), "stream not found")
}
