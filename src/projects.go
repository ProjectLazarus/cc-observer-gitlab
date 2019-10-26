package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/xanzy/go-gitlab"
)

type ProjectClient struct {
	Client *gitlab.Client
	ConcordClient
}

type ProjectTask struct {
	Namespace string `json:"project_namespace"`
	Name      string `json:"project_name"`
}

func (pc *ProjectClient) CancelTask(ct *ConcordTask) (err error) {
	log.Printf(
		"Cannot cancel project request %s. Feature is not yet implemented", ct.Id)
	return err
}

func (pc *ProjectClient) StartTask(ct *ConcordTask) error {
	if ct.Status != "pending" {
		log.Printf("Status is %s returning because it's not pending", ct.Status)
		return nil
	}
	if pc.RequestAck(ct.Key) {
		var pt ProjectTask
		err := json.Unmarshal(ct.Options, &pt)

		if err != nil {
			log.Printf("Failed to start task in %s err: %s", ct.Id, err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)

			return err
		}
		searchString := fmt.Sprintf("%s/%s", pt.Namespace, pt.Name)
		project, _, err := pc.Client.Projects.GetProject(searchString, nil)
		if project == nil {
			log.Println("Project does not exist creating...")
			opt := &gitlab.CreateProjectOptions{
				Name:        gitlab.String(pt.Name),
				Path:        gitlab.String(pt.Name),
				Description: gitlab.String("Project automatically generated created by Concord")}
			project, _, err = pc.Client.Projects.CreateProject(opt)
		} else {
			log.Printf("Project %s exists updating", project.Name)
			opt := &gitlab.EditProjectOptions{
				Name:        gitlab.String(pt.Name),
				Path:        gitlab.String(pt.Name),
				Description: gitlab.String("Project automatically generated created by Concord")}
			pc.Client.Projects.EditProject(project.ID, opt)
		}

		if err != nil {
			log.Printf("Could not create or update project. Error: %s", err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)
			return err
		}
		log.Printf("Project task completed %v", project)
		ct.Status = TaskStatusCompleted
		ok, err := pc.CompleteTask(ct)
		if !ok {
			log.Println("Encountered error completing task: ", err)
			ct.Status = TaskStatusError
			pc.CompleteTask(ct)
			return err
		}
	}

	return nil
}
