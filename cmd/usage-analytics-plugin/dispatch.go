package main

import (
	"context"
	"encoding/json"
	"github.com/JPSAUD501/CLIProxyAPI-Usage-Analytics-Plugin/internal/analytics"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"net/http"
)

const pluginVersion = "1.0.0"

var pluginService = analytics.New(callHost)

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}
type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  struct {
		UsagePlugin   bool `json:"usage_plugin"`
		ManagementAPI bool `json:"management_api"`
	} `json:"capabilities"`
}
type rpcManagementRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}
type registrationResponse struct {
	Routes    []pluginapi.ManagementRoute `json:"routes,omitempty"`
	Resources []pluginapi.ResourceRoute   `json:"resources,omitempty"`
}

func handleMethod(method string, request []byte) ([]byte, bool) {
	result, err := dispatch(method, request)
	if err != nil {
		return errorEnvelope("plugin_error", err.Error(), 500, false), true
	}
	raw, err := okEnvelope(result)
	if err != nil {
		return errorEnvelope("encoding_error", err.Error(), 500, false), true
	}
	return raw, false
}
func dispatch(method string, request []byte) (any, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var req lifecycleRequest
		if len(request) > 0 {
			if err := json.Unmarshal(request, &req); err != nil {
				return nil, err
			}
		}
		if err := pluginService.Configure(req.ConfigYAML); err != nil {
			return nil, err
		}
		return pluginRegistration(), nil
	case pluginabi.MethodUsageHandle:
		var record pluginapi.UsageRecord
		if err := json.Unmarshal(request, &record); err != nil {
			return nil, err
		}
		pluginService.HandleUsage(record)
		return struct{}{}, nil
	case pluginabi.MethodManagementRegister:
		r := pluginService.Registration()
		return registrationResponse{Routes: r.Routes, Resources: r.Resources}, nil
	case pluginabi.MethodManagementHandle:
		var req rpcManagementRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		return pluginService.Management(req.ManagementRequest)
	default:
		return nil, &methodError{method: method}
	}
}

type methodError struct{ method string }

func (e *methodError) Error() string { return "unknown plugin method: " + e.method }
func pluginRegistration() registration {
	r := registration{SchemaVersion: pluginabi.SchemaVersion, Metadata: pluginapi.Metadata{Name: "Usage Analytics", Version: pluginVersion, Author: "JPSAUD501", GitHubRepository: "https://github.com/JPSAUD501/CLIProxyAPI-Usage-Analytics-Plugin", ConfigFields: []pluginapi.ConfigField{{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Collect finalized request usage."}, {Name: "retention_days", Type: pluginapi.ConfigFieldTypeInteger, Description: "Retention period in days."}, {Name: "identity_mode", Type: pluginapi.ConfigFieldTypeString, Description: "Identity display mode; API keys are always HMAC protected."}}}}
	r.Capabilities.UsagePlugin = true
	r.Capabilities.ManagementAPI = true
	return r
}

var _ = context.Background
var _ = http.StatusOK
