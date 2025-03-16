package main

import (
	"errors"
	"net/rpc"
	"strconv"
	"time"

	"github.com/v1Flows/runner/pkg/executions"
	"github.com/v1Flows/runner/pkg/plugins"

	"github.com/v1Flows/alertFlow/services/backend/pkg/models"

	"github.com/hashicorp/go-plugin"
)

// Plugin is an implementation of the Plugin interface
type Plugin struct{}

func (p *Plugin) ExecuteTask(request plugins.ExecuteTaskRequest) (plugins.Response, error) {
	timeout := 0
	for _, param := range request.Step.Action.Params {
		if param.Key == "Timeout" {
			timeout, _ = strconv.Atoi(param.Value)
		}
	}

	err := executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
		ID: request.Step.ID,
		Messages: []string{
			`Waiting for user interaction`,
			`Timeout: ` + strconv.Itoa(timeout) + ` seconds`,
		},
		Interactive: true,
		Status:      "interactionWaiting",
		StartedAt:   time.Now(),
	})
	if err != nil {
		return plugins.Response{
			Success: false,
		}, err
	}

	executions.SetToInteractionRequired(request.Config, request.Execution)

	var stepData models.ExecutionSteps

	// pull current action status from backend every 10 seconds
	startTime := time.Now()
	for {
		stepData, err = executions.GetStep(request.Config, request.Execution.ID.String(), request.Step.ID.String())

		if stepData.Interacted {
			break
		} else {
			time.Sleep(5 * time.Second)
		}

		if err != nil {
			return plugins.Response{
				Success: false,
			}, err
		}

		if timeout > 0 && time.Since(startTime).Seconds() >= float64(timeout) {
			err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
				ID: request.Step.ID,
				Messages: []string{
					"Interaction timed out",
					"Automatically approved & continuing to the next step",
				},
				Status:              "success",
				FinishedAt:          time.Now(),
				Interacted:          true,
				InteractionApproved: true,
				InteractionRejected: false,
			})
			if err != nil {
				return plugins.Response{
					Success: false,
				}, err
			}

			stepData.Interacted = true
			stepData.InteractionApproved = true
			break
		}
	}

	executions.SetToRunning(request.Config, request.Execution)

	if stepData.InteractionRejected {
		err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
			ID: request.Step.ID,
			Messages: []string{
				"Interaction rejected",
				"Execution canceled",
			},
			Status:              "canceled",
			FinishedAt:          time.Now(),
			Interacted:          true,
			InteractionRejected: true,
			InteractionApproved: false,
		})
		if err != nil {
			return plugins.Response{
				Success: false,
			}, err
		}
		return plugins.Response{
			Data: map[string]interface{}{
				"status": "canceled",
			},
			Success: false,
		}, nil
	} else if stepData.InteractionApproved {
		err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
			ID:                  request.Step.ID,
			Messages:            []string{"Interaction approved"},
			Status:              "success",
			FinishedAt:          time.Now(),
			Interacted:          true,
			InteractionRejected: false,
			InteractionApproved: true,
		})
		if err != nil {
			return plugins.Response{
				Success: false,
			}, err
		}
		return plugins.Response{
			Success: true,
		}, nil
	}

	return plugins.Response{
		Success: true,
	}, nil
}

func (p *Plugin) HandleAlert(request plugins.AlertHandlerRequest) (plugins.Response, error) {
	return plugins.Response{
		Success: false,
	}, errors.New("not implemented")
}

func (p *Plugin) Info() (models.Plugins, error) {
	var plugin = models.Plugins{
		Name:    "Interaction",
		Type:    "action",
		Version: "1.1.1",
		Author:  "JustNZ",
		Actions: models.Actions{
			Name:        "Interaction",
			Description: "Wait for user interaction to continue",
			Plugin:      "interaction",
			Icon:        "solar:hand-shake-linear",
			Category:    "Utility",
			Params: []models.Params{
				{
					Key:         "Timeout",
					Type:        "number",
					Default:     "0",
					Required:    true,
					Description: "Continue to the next step after the specified time (in seconds). 0 to disable",
				},
			},
		},
		Endpoints: models.AlertEndpoints{},
	}

	return plugin, nil
}

// PluginRPCServer is the RPC server for Plugin
type PluginRPCServer struct {
	Impl plugins.Plugin
}

func (s *PluginRPCServer) ExecuteTask(request plugins.ExecuteTaskRequest, resp *plugins.Response) error {
	result, err := s.Impl.ExecuteTask(request)
	*resp = result
	return err
}

func (s *PluginRPCServer) HandleAlert(request plugins.AlertHandlerRequest, resp *plugins.Response) error {
	result, err := s.Impl.HandleAlert(request)
	*resp = result
	return err
}

func (s *PluginRPCServer) Info(args interface{}, resp *models.Plugins) error {
	result, err := s.Impl.Info()
	*resp = result
	return err
}

// PluginServer is the implementation of plugin.Plugin interface
type PluginServer struct {
	Impl plugins.Plugin
}

func (p *PluginServer) Server(*plugin.MuxBroker) (interface{}, error) {
	return &PluginRPCServer{Impl: p.Impl}, nil
}

func (p *PluginServer) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &plugins.PluginRPC{Client: c}, nil
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "PLUGIN_MAGIC_COOKIE",
			MagicCookieValue: "hello",
		},
		Plugins: map[string]plugin.Plugin{
			"plugin": &PluginServer{Impl: &Plugin{}},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
