package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/iodesystems/gwag/v2/gw/relay"

	"github.com/iodesystems/parasolve/internal/model"
)

// capTTL is how long a minted subscription cap lasts. The client mints again
// on each connect, so a cap only has to outlive the connect.
const capTTL = time.Hour

// liveChannel names the channel of a vault. The vault is in the name, so a cap
// for one vault never matches another's channel.
func liveChannel(vault string) string { return "events.para.vault." + vault }

// LiveEvent is the payload pushed to a client. It carries ids only: the client
// treats it as a signal to refetch, never as data.
type LiveEvent struct {
	Op  string `json:"op"`
	Doc string `json:"doc"`
}

// liveBus is the in-process source of the relay: a map from channel to the
// delivery functions of the sockets subscribed to it.
type liveBus struct {
	mu   sync.Mutex
	next int
	subs map[string]map[int]func([]byte)
}

func newLiveBus() *liveBus { return &liveBus{subs: map[string]map[int]func([]byte){}} }

// Subscribe implements relay.Source.
func (b *liveBus) Subscribe(channel string, deliver func([]byte)) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	if b.subs[channel] == nil {
		b.subs[channel] = map[int]func([]byte){}
	}
	b.subs[channel][id] = deliver
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[channel], id)
		if len(b.subs[channel]) == 0 {
			delete(b.subs, channel)
		}
	}, nil
}

// publish sends payload to every subscriber of channel. Delivery only enqueues,
// so it does not block the writer that appended the event.
func (b *liveBus) publish(channel string, payload []byte) {
	b.mu.Lock()
	fns := make([]func([]byte), 0, len(b.subs[channel]))
	for _, f := range b.subs[channel] {
		fns = append(fns, f)
	}
	b.mu.Unlock()
	for _, f := range fns {
		f(payload)
	}
}

// Live pushes vault events to browsers over /ws. One relay serves every vault.
type Live struct {
	bus    *liveBus
	relay  *relay.Relay
	secret []byte
}

// NewLive returns the live relay. Give it to the Hub and to Options.
func NewLive() (*Live, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("server: live: %w", err)
	}
	bus := newLiveBus()
	// No compression: it is untested on Safari and iOS, and an event is tiny.
	return &Live{bus: bus, secret: secret, relay: relay.New(secret, relay.Source(bus))}, nil
}

// Handler is the WebSocket endpoint.
func (l *Live) Handler() http.Handler { return l.relay.Handler() }

// Notify is the vault.Options.OnEvent of a vault. It runs under the vault lock
// and returns at once.
func (l *Live) Notify(vaultID string) func(model.Event) {
	return func(e model.Event) {
		b, err := json.Marshal(LiveEvent{Op: e.Op, Doc: e.Doc})
		if err != nil {
			return
		}
		l.bus.publish(liveChannel(vaultID), b)
	}
}

// Mint signs a cap for one vault's channel.
func (l *Live) Mint(vaultID string, now time.Time) (token, channel string, exp int64, err error) {
	channel = liveChannel(vaultID)
	exp = now.Add(capTTL).Unix()
	token, err = relay.Sign(l.secret, relay.Cap{Channel: channel, Exp: exp})
	if err != nil {
		return "", "", 0, fmt.Errorf("server: live: mint: %w", err)
	}
	return token, channel, exp, nil
}

// Shutdown closes every socket with close code 1001 and waits for the relay to
// finish, or for ctx to end.
func (l *Live) Shutdown(ctx context.Context) error { return l.relay.Shutdown(ctx) }

// RegisterShutdown hooks the relay to srv's shutdown. http.Server.Shutdown does
// not touch hijacked connections, so without this every socket ends as 1006.
// srv runs the hook in its own goroutine without waiting, so the returned
// channel closes when the relay is done and the caller waits on it.
func (l *Live) RegisterShutdown(srv *http.Server, timeout time.Duration, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	srv.RegisterOnShutdown(func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := l.relay.Shutdown(ctx); err != nil {
			log.Warn("live relay shutdown timed out", "error", err)
		}
	})
	return done
}

// GenChannels is the registry cmd/livegen walks to write the typed client in
// ui/src/gen. Adding a channel here is all the payload types need.
var GenChannels = []relay.ChannelSpec{
	{Name: "Vault", Event: reflect.TypeFor[LiveEvent]()},
}

// GenEnums declares the value sets of named string types in event payloads.
var GenEnums = []relay.EnumSpec{}
