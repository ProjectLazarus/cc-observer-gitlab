package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

var (
	StatusChangeNotifierHost = os.Getenv("CONCORD_STATUS_CHANGE_NOTIFIER_HOST")
)

var (
	DuplicateListenerError = errors.New("a callback exists for this event type")
	ListenterNotFoundError = errors.New("a listener was not found for the event")
)

// Conn contains methods for interacting with websocket types.
type Conn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
}

// Event contains the details of a status change event.
type Event struct {
	// Kind is the type of status change event.
	// Created is the time the event occured.
	// Meta is passthrough data about the event.
	Kind    string          `json:"kind"`
	Created time.Time       `json:"created"`
	Meta    json.RawMessage `json:"meta'`
}

// NewEvent create a new event instance from the provided data.
func NewEvent(kind string, meta []byte) *Event {
	return &Event{Kind: kind, Created: time.Now(), Meta: meta}
}

// Observer schedules staged tasks in gitlab pods for execution.
type Observer struct {
	conn      Conn
	events    []string
	listeners map[string]func(json.RawMessage)
}

// NewObserver creates a new observer instance.
func NewObserver() *Observer {
	return &Observer{listeners: make(map[string]func(json.RawMessage))}
}

// AddListener adds the callback for the provided event kind.
func (obs *Observer) AddListener(kind string, callback func(json.RawMessage)) error {
	log.Printf("Adding listener for event %s", kind)
	if _, ok := obs.listeners[kind]; ok {
		return DuplicateListenerError
	}
	obs.listeners[kind] = callback
	return nil
}

// Connect establishes a websocket client connection to the status
// change notifier.
func (obs *Observer) Connect() error {
	url := fmt.Sprintf("ws://%s/observers", StatusChangeNotifierHost)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return err
	}
	obs.conn = c
	if err := obs.SendEvents(); err != nil {
		return err
	}
	return nil
}

func (obs *Observer) ListenForEvents(events chan<- []byte) {

	for {
		messageType, p, err := obs.conn.ReadMessage()
		if err != nil {
			log.Println("err", err)
			break
		}
		if messageType > 0 {
			log.Println("messageType: ", messageType)
		}
		events <- p
	}
}

// HandleEvents executes the callback of the provided event.
//
// If no callback is found for the event the event is skipped.
func (obs *Observer) HandleEvents(events chan []byte) {
	for eventBytes := range events {
		event := &Event{}
		json.Unmarshal(eventBytes, event)
		if callback, ok := obs.listeners[event.Kind]; ok {
			callback(event.Meta)
		} else {
			log.Println("handler error:", ListenterNotFoundError)
		}
	}
}

// SendEvents sends the list of events to the status change notifier
// that the observer wants to subscribe to.
func (obs *Observer) SendEvents() error {
	events, _ := json.Marshal(obs.events)
	if err := obs.conn.WriteMessage(websocket.BinaryMessage, events); err != nil {
		return err
	}
	return nil
}
