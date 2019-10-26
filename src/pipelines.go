package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xanzy/go-gitlab"
)

const (
	triggerName = "concordTrigger"
)

type PipelineClient struct {
	Client *gitlab.Client
	ConcordClient
}

type ProjectData struct {
	Id   int    `json:"id"`
	Path string `json:"path"`
}

type RequestData struct {
	ProjectData *ProjectData      `json:"project"`
	Ref         string            `json:"ref"`
	Token       string            `json:"token"` // Optional gitlab trigger token be passed in.
	Variables   map[string]string `json:"variables"`
}

type PipelineTask struct {
	RequestData *RequestData
	ProjectId   string
	ProjectName string
}

func (pc *PipelineClient) SetProjectId(pt *PipelineTask) (err error) {
	// Given an path or id set the "ID" we're going to send to Gitlab.
	// ProjectData.Id is the preference.

	pd := pt.RequestData.ProjectData

	if pd.Id > 0 {
		pt.ProjectId, err = parseID(pd.Id)
		if err != nil {
			return err
		}
	} else if pd.Path != "" {
		pt.ProjectId = pd.Path
		pathStrings := strings.Split(pd.Path, "/")
		if len(pathStrings) <= 1 {
			err = fmt.Errorf("Could not set project ID from path %s. Invalid path", pd.Path)
		}
		// The project name should be at the end of the path.
		pt.ProjectName = pathStrings[len(pathStrings)-1]
	} else {
		err = fmt.Errorf(
			"Could not set project ID from project data id %v or path %s. Invalid data",
			pd.Id, pd.Path)
	}
	return err
}

// This function is a re-implementation of future upstream code.
// It can be removed once this lands https://github.com/xanzy/go-gitlab/pull/673
func (pc *PipelineClient) GetPipelineVars(projectId interface{}, pipeline int, options ...gitlab.OptionFunc) ([]*gitlab.PipelineVariable, *gitlab.Response, error) {
	project, err := parseID(projectId)
	if err != nil {
		return nil, nil, err
	}

	u := fmt.Sprintf("projects/%s/pipelines/%d/variables", pathEscape(project), pipeline)

	req, err := pc.Client.NewRequest("GET", u, nil, options)
	if err != nil {
		return nil, nil, err
	}

	var pv []*gitlab.PipelineVariable
	resp, err := pc.Client.Do(req, &pv)
	if err != nil {
		return nil, resp, err
	}

	return pv, resp, err
}

func (pc *PipelineClient) ValidateRef(pt PipelineTask) (valid bool, err error) {
	valid = false
	branches, _, err := pc.Client.Branches.ListBranches(pt.ProjectId, nil, nil)
	for _, branch := range branches {
		log.Printf("Task ref: %s branch name %s", pt.RequestData.Ref, branch.Name)
		if pt.RequestData.Ref == branch.Name {
			// log.Printf("Task ref: %s branch name %s", pt.RequestData.Ref, branch.Name)
			valid = true
		}
	}

	return valid, err
}

func (pc *PipelineClient) CancelTask(ct *ConcordTask) (err error) {
	log.Println("Attempting to cancel Concord pipeline task :", ct.Id)
	var pt PipelineTask
	err = json.Unmarshal(ct.Options, &pt.RequestData)
	if err != nil {
		return err
	}
	err = pc.SetProjectId(&pt)
	if err != nil {
		return err
	}
	pipelines, _, err := pc.Client.Pipelines.ListProjectPipelines(pt.ProjectId, nil)
	if err != nil {
		log.Printf("Failed to retrieve pipelines for cancel task. Error: %s", err)
		return err
	}

	for _, pipeline := range pipelines {
		if pipeline.Status == "running" {
			pipelineVars, _, err := pc.GetPipelineVars(pt.ProjectId, pipeline.ID, nil)
			if err != nil {
				return err
			}
			for _, pipelineVar := range pipelineVars {
				if pipelineVar.Key == "concord_task_id" && pipelineVar.Value == pt.RequestData.Variables["concord_task_id"] {
					cancelled, _, err := pc.Client.Pipelines.CancelPipelineBuild(pt.ProjectId, pipeline.ID)
					if err != nil {
						log.Printf(
							"Failed to cancel task %s. Pipeline ID: %v",
							pt.RequestData.Variables["concord_task_id"], pipeline.ID)
						return err
					}
					log.Printf(
						"Successfully cancelled task %s. Pipeline ID: %v Status %s",
						pt.RequestData.Variables["concord_task_id"], cancelled.ID, cancelled.Status)
				}
			}
		}
	}
	return nil
}

func (pc *PipelineClient) GetTrigger(projectId interface{}) (trigger *gitlab.PipelineTrigger, err error) {
	triggers, _, err := pc.Client.PipelineTriggers.ListPipelineTriggers(projectId, nil)
	found := false
	for _, trigger := range triggers {
		if trigger.Description == triggerName {
			found = true
			log.Println("Found trigger: ", &trigger, "token: ", trigger.Token)
			return trigger, nil
		}
	}

	if !found {
		log.Println("Trigger not found for Concord. Creating one...")
		opt := &gitlab.AddPipelineTriggerOptions{Description: gitlab.String(triggerName)}
		trigger, _, err = pc.Client.PipelineTriggers.AddPipelineTrigger(projectId, opt)
		if err != nil {
			return nil, err
		}
	}

	return trigger, err
}

func (pc *PipelineClient) StartTask(ct *ConcordTask) (err error) {
	if ct.Status != "pending" {
		return nil
	}

	if pc.RequestAck(ct.Key) {
		var pt PipelineTask
		err = json.Unmarshal(ct.Options, &pt.RequestData)
		if err != nil {
			return err
		}

		err = pc.SetProjectId(&pt)
		if err != nil {
			return err
		}

		// Set the concord_id in trigger vars so we can identify it.
		pt.RequestData.Variables["concord_task_id"] = ct.Id

		if err != nil {
			log.Printf("Failed to start task in %s err: %s", ct.Id, err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)

			return err
		}

		if pt.RequestData.Ref == "" {
			pt.RequestData.Ref = "master"
		}
		validRef, err := pc.ValidateRef(pt)
		if err != nil {
			log.Printf("Failed to start task in %s err: %s", ct.Id, err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)

			return err
		}
		if !validRef {
			err = fmt.Errorf(
				"Specified ref %s was not found in project %s.",
				pt.RequestData.Ref, pt.ProjectId)
			log.Println(err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)

			return err
		}
		pipeline, err := pc.TriggerPipeline(pt)

		if err != nil {
			log.Printf("Failed to start task in %s err: %s", ct.Id, err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)
			return err
		}

		if !ct.Async {
			status, err := pc.WatchUntilComplete(pt.ProjectId, pipeline.ID)
			if err != nil {
				return err
			}
			log.Printf("WatchUntilComplete status: %s", status)
			ct.Status = status
			ok, err := pc.CompleteTask(ct)
			if !ok {
				log.Println("Encountered error completing task: ", err)
				return err
			}
		}
	}

	return nil
}

func (pc *PipelineClient) GetPipelineStatus(projectId interface{}, pipeLineId int) (string, error) {
	p, _, err := pc.Client.Pipelines.GetPipeline(projectId, pipeLineId)
	if err != nil {
		return "", err
	}

	return p.Status, err
}

func (pc *PipelineClient) TriggerPipeline(pt PipelineTask) (*gitlab.Pipeline, error) {
	trigger, err := pc.GetTrigger(pt.ProjectId)
	if err != nil {
		return nil, err
	}
	opt := &gitlab.RunPipelineTriggerOptions{
		Ref:       gitlab.String(pt.RequestData.Ref),
		Token:     gitlab.String(trigger.Token),
		Variables: pt.RequestData.Variables}
	log.Printf(
		"Triggering pipeline for project %s on Ref %s",
		pt.ProjectId, pt.RequestData.Ref)
	p, _, err := pc.Client.PipelineTriggers.RunPipelineTrigger(pt.ProjectId, opt)
	if err != nil {
		return nil, err
	}
	if p == nil {
		err = fmt.Errorf("Could not start pipeline with pipeline task %v.", pt)
		return nil, err
	}

	return p, nil
}

func (pc *PipelineClient) WatchUntilComplete(projectId interface{}, pipeLineId int) (string, error) {
	for {
		s, err := pc.GetPipelineStatus(projectId, pipeLineId)
		if err != nil {
			log.Printf("Could not retrieve pipeline status. Error: %q", err)
			return ConcordStatus("failed"), err
		}
		log.Println("Pipeline ", pipeLineId, "Status", s)
		if IsFinished(s) {
			log.Printf("Pipeline %v finished with status %s", pipeLineId, s)
			return ConcordStatus(s), nil
		}
		log.Printf("Pipeline %v is still running. Status: %s", pipeLineId, s)
		time.Sleep(10 * time.Second)
	}
}
