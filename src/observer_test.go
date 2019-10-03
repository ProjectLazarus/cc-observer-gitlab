package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/gorilla/websocket"
)

type ObserverHandler struct {
	OnMessage func(message []byte)
}

func (obs ObserverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal("upgrade:", err)
		return
	}
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Fatal(err)
		}
		obs.OnMessage(message)
	}

	c.Close()
}

func TestObserverConnect(t *testing.T) {
	recv := make(chan []byte)
	obs := &Observer{events: []string{"someEventType"}}
	if err := obs.Connect(); err == nil {
		t.Fatal("expected connect error")
	}
	s := &http.Server{
		Addr: ":5555",
		Handler: ObserverHandler{
			OnMessage: func(message []byte) {
				recv <- message
			},
		},
	}
	defer s.Close()
	go func() {
		s.ListenAndServe()
	}()
	connected := false
	for i := 0; i < 10; i++ {
		if err := obs.Connect(); err != nil {
			time.Sleep(time.Second)
			continue
		}
		connected = true
		break
	}
	if !connected {
		t.Fatal("connection failed")
	}
	if string(<-recv) != string(`["someEventType"]`) {
		t.Fatal("Invalid connect message received")
	}
}

func TestObserverSendEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	var table = []struct {
		Events []string
		Bytes  []byte
		Err    error
	}{
		{
			[]string{"ev1", "ev3", "ev2"},
			[]byte(`["ev1","ev3","ev2"]`),
			nil,
		},
		{
			[]string{"ev00", "ev03", "ev02"},
			[]byte(`["ev00","ev03","ev02"]`),
			errors.New("write error"),
		},
	}
	for _, tt := range table {
		conn := NewMockConn(ctrl)
		conn.
			EXPECT().
			WriteMessage(2, tt.Bytes).
			Return(tt.Err).
			Times(2)
		obs := &Observer{events: tt.Events}
		obs.conn = conn
		if err := obs.SendEvents(); tt.Err != nil && err.Error() != tt.Err.Error() {
			t.Fatal(err)
		}
		// conn.AssertExpectations(t)
	}
}

func TestObserverAddListener(t *testing.T) {
	calls := []bool{false, false}
	var table = []struct {
		EvtKind  string
		Callback func(json.RawMessage)
	}{
		{
			"thing1",
			func(json.RawMessage) {
				calls[0] = true
			},
		},
		{
			"thing2",
			func(json.RawMessage) {
				calls[1] = true
			},
		},
	}

	for i, tt := range table {
		obs := NewObserver()
		if err := obs.AddListener(tt.EvtKind, tt.Callback); err != nil {
			t.Fatal(err)
		}
		if err := obs.AddListener(tt.EvtKind, tt.Callback); err != DuplicateListenerError {
			t.Fatal("expected duplicate listener error")
		}
		if _, ok := obs.listeners[tt.EvtKind]; !ok {
			t.Fatal("expected callback to have been added for event kind")
		}
		var meta json.RawMessage = []byte("")
		obs.listeners[tt.EvtKind](meta)
		if !calls[i] {
			t.Fatal("expected callback to have been called")
		}
	}
}

func TestObserverHandleEvents(t *testing.T) {
	calls := [4]bool{}
	var table = []struct {
		EvtKind  string
		Callback func(json.RawMessage)
		Event    []byte
		Called   bool
		ReadErr  error
	}{
		{
			"doSomething",
			func(meta json.RawMessage) { calls[0] = true },
			[]byte(`{"kind": "doSomething"}`),
			true,
			nil,
		},
		{
			"doAnother",
			func(meta json.RawMessage) { calls[1] = true },
			[]byte(`{"kind": "doAnother"}`),
			true,
			nil,
		},
		{
			"doAnother",
			func(meta json.RawMessage) { calls[1] = true },
			[]byte(`{"kind": "doAnother"}`),
			false,
			errors.New("read error"),
		},
		{
			"doThis",
			func(meta json.RawMessage) { calls[3] = true },
			[]byte(`{"kind": "doThat"}`),
			false,
			nil,
		},
	}

	for i, tt := range table {
		obs := NewObserver()
		if err := obs.AddListener(tt.EvtKind, tt.Callback); err != nil {
			t.Fatal(err)
		}
		events := make(chan []byte)
		go func() {
			events <- tt.Event
			close(events)
		}()
		obs.HandleEvents(events)
		if calls[i] != tt.Called {
			t.Fatalf("expected called to be %v", tt.Called)
		}
	}
}
