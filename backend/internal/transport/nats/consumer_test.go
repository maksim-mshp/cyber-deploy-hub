package natsbus

import (
	"testing"
	"time"
)

func TestNormalizeConsumerOptionsDefaults(t *testing.T) {
	opts := normalizeConsumerOptions(ConsumerOptions{})

	if opts.Subject != "cmd.>" {
		t.Fatalf("Subject = %q", opts.Subject)
	}
	if opts.Stream != "COMMANDS" {
		t.Fatalf("Stream = %q", opts.Stream)
	}
	if opts.Durable != "default" {
		t.Fatalf("Durable = %q", opts.Durable)
	}
	if opts.MaxDeliver != 5 {
		t.Fatalf("MaxDeliver = %d", opts.MaxDeliver)
	}
}

func TestNormalizeConsumerOptionsUsesEventsStream(t *testing.T) {
	opts := normalizeConsumerOptions(ConsumerOptions{Subject: "evt.lab.>"})
	if opts.Stream != "EVENTS" {
		t.Fatalf("Stream = %q, want EVENTS", opts.Stream)
	}
}

func TestConsumerBackoffDelayCapsAtMax(t *testing.T) {
	got := backoffDelay(8, time.Second, 5*time.Second)
	if got != 5*time.Second {
		t.Fatalf("backoffDelay = %v, want 5s", got)
	}
}
