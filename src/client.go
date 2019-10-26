package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/bitwurx/jrpc2"
	"github.com/xanzy/go-gitlab"
)

const (
	BrokerCallErrorCode jrpc2.ErrorCode = -32100 // broker call jrpc error code.
	GitLabBaseUrlEnv                    = "GITLAB_BASE_URL"
	GitLabTokenEnv                      = "GITLAB_TOKEN"
	TaskStatusError                     = "error"
	TaskStatusCompleted                 = "completed"
)

var (
	ControllerHost        = os.Getenv("CONCORD_CONTROLLER_HOST")
	PipelineNotFoundError = errors.New("No pipeline could be found with that name/id")
)

type PipeStatus struct {
	Status        string
	ConcordStatus string
	IsFinished    bool
}

type ConcordClient struct {
	broker ServiceBroker
}

type GitlabClient interface {
	StartTask(*ConcordTask) error
	CancelTask(*ConcordTask) error
}

// ServiceBroker contains method for calling external services.
type ServiceBroker interface {
	Call(string, string, map[string]interface{}) (interface{}, *jrpc2.ErrorObject)
}

// JsonRPCServiceBroker is json-rpc 2.0 service broker.
type JsonRPCServiceBroker struct{}

// Concord specific task data derived from Event.meta
type ConcordTask struct {
	Id      string          `json:"_id"`
	Status  string          `json:"_status"`
	Key     string          `json:"_key"`
	Service string          `json:"service"`
	Type    string          `json:"type"`
	Async   bool            `json:"async"` // Toggles whether the observer should watch and handle status.
	Options json.RawMessage `json:"options"`
}

func (t *JsonRPCServiceBroker) Call(
	url string,
	method string,
	params map[string]interface{}) (interface{}, *jrpc2.ErrorObject) {

	p, _ := json.Marshal(params)

	req := bytes.NewBuffer([]byte(fmt.Sprintf(
		`{
			"jsonrpc": "2.0",
			"method": "%s", "params": %s,
			"id": 0}`, method, string(p))))

	resp, err := http.Post(fmt.Sprintf(
		"http://%s/rpc", url), "application/json", req)
	if err != nil {
		return nil, &jrpc2.ErrorObject{
			Code:    BrokerCallErrorCode,
			Message: jrpc2.ServerErrorMsg,
			Data:    err.Error(),
		}
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, &jrpc2.ErrorObject{
			Code:    BrokerCallErrorCode,
			Message: jrpc2.ServerErrorMsg,
			Data:    err.Error(),
		}
	}

	var respObj jrpc2.ResponseObject
	json.Unmarshal(body, &respObj)

	return respObj.Result, respObj.Error
}

func IsFinished(status string) bool {
	finishedStates := map[string]bool{
		"failed":    true,
		"cancelled": true,
		"canceled":  true,
		"success":   true,
		"manual":    true,
		"skipped":   true,
	}

	return finishedStates[status]
}

func ConcordStatus(status string) string {
	concordStates := map[string]string{
		"failed":    "error",
		"cancelled": "cancelled",
		"canceled":  "cancelled",
		"success":   "complete",
		"manual":    "manual",
		"skipped":   "skipped",
	}

	return concordStates[status]
}

func (c *ConcordClient) RequestAck(key string) bool {
	if key == "" {
		log.Println("key is null!")
		return false
	}
	params := map[string]interface{}{"key": key}
	result, errObj := c.broker.Call(ControllerHost, "startTask", params)
	if errObj != nil {
		return false

		log.Println(errors.New(string(errObj.Message)))
	}
	result = int(result.(float64))

	// A succes result code should be 0
	// Anything else means this is not an actionable event
	if result != 0 {
		return false
	}

	return true

}

func NewGitlabClient(taskType string) (gc GitlabClient, err error) {
	apiToken, err := ValidateEnv(GitLabTokenEnv)

	if err != nil {
		return nil, err
	}

	git := gitlab.NewClient(nil, apiToken)
	baseUrl, err := ValidateEnv(GitLabBaseUrlEnv)

	if err != nil {
		return nil, err
	}

	err = git.SetBaseURL(baseUrl)
	if err != nil {
		return nil, err
	}

	switch taskType {
	case "pipeline":
		pc := &PipelineClient{Client: git}
		pc.ConcordClient = ConcordClient{broker: &JsonRPCServiceBroker{}}
		gc = pc
	case "project":
		pc := &ProjectClient{Client: git}
		pc.ConcordClient = ConcordClient{broker: &JsonRPCServiceBroker{}}
		gc = pc

	default:
		err = fmt.Errorf(
			"JSON error: Could not find a client for type %s", taskType)
	}
	return gc, err
}

func (c *ConcordClient) CompleteTask(ct *ConcordTask) (bool, error) {

	params := map[string]interface{}{"id": ct.Id, "status": ct.Status}

	result, errObj := c.broker.Call(ControllerHost, "completeTask", params)
	if errObj != nil {
		return false, errors.New(string(errObj.Message))
	}
	result = int(result.(float64))

	// A succes result code should be 0
	// Anything else means this is not an actionable event
	if result != 0 {
		return false, errors.New(fmt.Sprintf("Broker call was not 0. Was %v", result))
	}
	log.Printf("Task %s completed with status %s", ct.Id, ct.Status)
	return true, nil
}
