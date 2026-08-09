package dynamicflow

import (
	"takt/internal/application"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/hostcontrol"
	"takt/internal/extensions/blockcatalog"
)

type RunSummary = application.RunSummary
type WorkflowListEntry = application.WorkflowListEntry
type AdapterPreflightStatus = blockcatalog.PreflightStatus
type StartRequest = application.StartRequest
type RetryRequest = application.RetryRequest
type PlanRecord = dynamicplan.Record
type HostCapabilities = hostcontrol.Capabilities
type HostSession = hostcontrol.Session

const HostEnforcementAdvisory = hostcontrol.EnforcementAdvisory
