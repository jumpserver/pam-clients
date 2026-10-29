package pam

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// EventHandlers run serially, independently of the WebSocket reader. Credential
// handlers must be idempotent. Only the application may confirm a successful switch.
type EventHandlers struct {
	// OnAccountChanged opts into managed account events. Return only after
	// connection validation, pool switching and old connection cleanup.
	OnAccountChanged     func(context.Context, Account) error
	OnAccountRevoked     func(context.Context, string) error
	OnApplicationRestart func(context.Context) error
	OnEvent              func(context.Context, Event) error
	OnCredentialChanged  func(context.Context, Credential) error
	OnCredentialRevoked  func(context.Context, Event) error
	OnError              func(context.Context, error, *Event)
}

// EventWatcher owns one managed event listener. Stop requests cancellation;
// Wait waits for handlers and the reader. Do not call Wait from a handler.
type EventWatcher struct {
	client             *Client
	ctx                context.Context
	cancel             context.CancelFunc
	handlers           EventHandlers
	done               chan struct{}
	err                error
	pending            map[CredentialSelector]pendingCredential
	pendingRevocations map[CredentialSelector]pendingRevocation
	appliedEvents      map[string]accountEventResult
	appliedOrder       []string
	usedAccounts       map[CredentialSelector]string
	accountScope       map[CredentialSelector]bool
}

type pendingCredential struct {
	update Event
	event  Event
	delay  time.Duration
	due    time.Time
}

type pendingRevocation struct {
	accountID string
	event     Event
	delay     time.Duration
	due       time.Time
}

func (c *Client) StartEvents(ctx context.Context, handlers EventHandlers) (*EventWatcher, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("client is closed")
	}
	if c.watcher != nil {
		select {
		case <-c.watcher.done:
		default:
			return nil, errors.New("an event listener is already running")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &EventWatcher{client: c, ctx: ctx, cancel: cancel, handlers: handlers,
		done: make(chan struct{}), pending: make(map[CredentialSelector]pendingCredential),
		appliedEvents: make(map[string]accountEventResult), usedAccounts: make(map[CredentialSelector]string)}
	c.watcher = w
	go w.run()
	return w, nil
}

// WatchEvents blocks until ctx is canceled, Client.Close is called, or the reader
// fails. Credential handler errors are reported and retried, not returned here.
func (c *Client) WatchEvents(ctx context.Context, handlers EventHandlers) error {
	w, err := c.StartEvents(ctx, handlers)
	if err != nil {
		return err
	}
	return w.Wait()
}

func (w *EventWatcher) Stop() { w.cancel() }
func (w *EventWatcher) Wait() error {
	<-w.done
	return w.err
}

func (w *EventWatcher) report(err error, event *Event) {
	if w.ctx.Err() != nil {
		return
	}
	if w.handlers.OnError != nil {
		if failure := invokeEventHook(func() error { w.handlers.OnError(w.ctx, err, event); return nil }); failure != nil {
			log.Printf("Credential error hook failed: %T", failure)
		}
	} else {
		// Exception messages and event payloads can contain credential material.
		log.Printf("Credential event handler failed: %T", err)
	}
}

// A panic in an application callback must not crash the application's process.
// Do not include the recovered value; it may contain credentials.
func invokeEventHook(hook func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("application event hook panicked")
		}
	}()
	return hook()
}

func eventSelector(event Event) (CredentialSelector, bool) {
	key := event.CredentialKey
	if key == "" {
		key = event.Key
	}
	if event.CredentialMode == "subscription" && key != "" && event.AccountID != "" {
		if !strings.HasSuffix(key, ":"+event.AccountID) {
			key += ":" + event.AccountID
		}
		return CredentialSelector{Key: key}, true
	}
	if event.CredentialMode == "alternating_rotation" && key != "" {
		return CredentialSelector{Key: key}, true
	}
	return CredentialSelector{}, false
}

func (w *EventWatcher) refresh(update, event Event, delay time.Duration) {
	if w.handlers.OnAccountChanged != nil {
		w.refreshAccount(update, event, delay)
		return
	}
	selector, valid := eventSelector(update)
	if !valid || w.ctx.Err() != nil {
		return
	}
	if previous, exists := w.pending[selector]; exists && previous.update.Revision > update.Revision {
		return
	}
	credential, err := w.client.GetCredentialFresh(w.ctx, selector)
	if err == nil && credential.Revision < update.Revision {
		err = errors.New("credential API revision is behind the event")
	}
	if err == nil && w.ctx.Err() == nil && w.handlers.OnCredentialChanged != nil {
		err = invokeEventHook(func() error { return w.handlers.OnCredentialChanged(w.ctx, credential) })
	}
	if w.ctx.Err() != nil {
		return
	}
	if err != nil {
		w.report(err, &event)
		delay *= 2
		if delay < time.Second {
			delay = time.Second
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		w.pending[selector] = pendingCredential{update, event, delay, time.Now().Add(delay)}
	} else {
		delete(w.pending, selector)
	}
}

func (w *EventWatcher) dispatch(event Event) {
	event.Event = canonicalEventName(event.Event)
	if event.Event == "snapshot" || event.Event == "credential.revoked" || event.Event == "app.scope.updated" || event.Event == "configuration.updated" {
		clear(w.pending)
	}
	if w.handlers.OnEvent != nil {
		if err := invokeEventHook(func() error { return w.handlers.OnEvent(w.ctx, event) }); err != nil {
			w.report(err, &event)
		}
	}
	if w.ctx.Err() != nil {
		return
	}
	switch event.Event {
	case "snapshot":
		if w.handlers.OnAccountChanged != nil {
			retained := make(map[CredentialSelector]bool)
			for _, update := range event.Credentials {
				if selector, ok := accountEventSelector(update); ok {
					retained[selector] = true
				}
			}
			w.accountScope = make(map[CredentialSelector]bool, len(retained))
			for selector := range retained {
				w.accountScope[selector] = true
			}
			for _, id := range event.AuthorizedAccountIDs {
				retained[CredentialSelector{AccountID: id}] = true
			}
			w.revokeAccounts(retained, event)
		}
		for _, update := range event.Credentials {
			w.refresh(update, event, 0)
		}
	case "app.scope.updated", "configuration.updated":
		if w.handlers.OnAccountChanged != nil {
			w.accountScope = make(map[CredentialSelector]bool)
			if event.AuthorizedAccountIDs != nil {
				allowed := make(map[string]bool)
				retained := make(map[CredentialSelector]bool)
				for _, id := range event.AuthorizedAccountIDs {
					allowed[id] = true
					retained[CredentialSelector{AccountID: id}] = true
				}
				for selector, id := range w.usedAccounts {
					if allowed[id] {
						retained[selector] = true
					}
				}
				w.revokeAccounts(retained, event)
			}
		}
	case "credential.updated":
		if event.CommandID == "" || w.handlers.OnAccountChanged != nil {
			w.refresh(event, event, 0)
		}
	case "credential.revoked":
		if w.handlers.OnAccountChanged != nil {
			retained := make(map[CredentialSelector]bool)
			key := event.CredentialKey
			if key == "" {
				key = event.Key
			}
			delete(w.accountScope, CredentialSelector{AccountID: event.AccountID})
			delete(w.accountScope, CredentialSelector{Key: "policy:" + key})
			for selector, id := range w.usedAccounts {
				if id != event.AccountID && selector.Key != "policy:"+key {
					retained[selector] = true
				}
			}
			w.revokeAccounts(retained, event)
		}
		if w.handlers.OnCredentialRevoked != nil {
			if err := invokeEventHook(func() error { return w.handlers.OnCredentialRevoked(w.ctx, event) }); err != nil {
				w.report(err, &event)
			}
		}
	case "credential.switch.requested", "app.restart.requested":
		if w.handlers.OnAccountChanged != nil && event.CommandID != "" {
			w.refresh(event, event, 0)
		}
	}
}

func (w *EventWatcher) retry() {
	var earliest *pendingCredential
	for _, item := range w.pending {
		if earliest == nil || item.due.Before(earliest.due) {
			copy := item
			earliest = &copy
		}
	}
	var revoked CredentialSelector
	var cleanup *pendingRevocation
	for selector, item := range w.pendingRevocations {
		if cleanup == nil || item.due.Before(cleanup.due) {
			copy := item
			revoked, cleanup = selector, &copy
		}
	}
	if cleanup != nil && (earliest == nil || !cleanup.due.After(earliest.due)) {
		if !cleanup.due.After(time.Now()) {
			w.revokeAccount(revoked, cleanup.accountID, cleanup.event, cleanup.delay)
		}
		return
	}
	if earliest != nil && !earliest.due.After(time.Now()) {
		w.refresh(earliest.update, earliest.event, earliest.delay)
	}
}

func (w *EventWatcher) run() {
	release := context.AfterFunc(w.client.stop, w.cancel)
	defer release()
	if w.client.stop.Err() != nil {
		w.cancel()
	}
	events := make(chan Event, 128)
	readerDone := make(chan struct{})
	var readerErr error
	go func() {
		readerErr = w.client.WatchCredentialEvents(w.ctx, func(event Event) error {
			select {
			case events <- event:
				return nil
			case <-w.ctx.Done():
				return w.ctx.Err()
			}
		})
		close(events)
		close(readerDone)
	}()
	defer func() {
		w.cancel()
		<-readerDone
		clear(w.pending)
		clear(w.pendingRevocations)
		close(w.done)
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			w.err = w.ctx.Err()
			return
		case event, open := <-events:
			if !open {
				<-readerDone
				w.err = readerErr
				if w.err != nil {
					w.report(w.err, nil)
				}
				return
			}
			w.dispatch(event)
			w.retry()
		case <-ticker.C:
			w.retry()
		}
	}
}
