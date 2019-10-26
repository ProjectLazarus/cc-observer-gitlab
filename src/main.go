package main

import (
	"encoding/json"
	"log"
	"time"
)

const (
	RetryInterval = 30
)

func CheckEvent(message json.RawMessage) {
	var ct ConcordTask
	err := json.Unmarshal(message, &ct)
	if err != nil {
		log.Fatal(err)
	}
	if ct.Service != "gitlab" {
		return
	}
	log.Printf("Received event: ID: %s Type: %s Status: %s\n",
		ct.Id, ct.Type, ct.Status)
	gl, err := NewGitlabClient(ct.Type)
	if err != nil {
		log.Printf("Could not create Gitlab client Error: %q", err)
	}
	go gl.StartTask(&ct)
}

func main() {
	obs := NewObserver()
	obs.events = []string{"taskStatusChanged"}

	// Try to connect every 30 seconds until we're successful.
	for {
		if err := obs.Connect(); err != nil {
			log.Printf("Encountered while attempting to connect to the status change notifier.")
			log.Printf("Error: %v", err)
			log.Printf("Retrying in %v seconds.", RetryInterval)
			time.Sleep(RetryInterval * time.Second)

		} else {
			log.Println("connected")
			break
		}
	}
	// Listen for taskStatusChanged events with the
	// CheckEvent function
	obs.AddListener("taskStatusChanged", CheckEvent)
	events := make(chan []byte)
	go obs.ListenForEvents(events)
	obs.HandleEvents(events)
}
