package core

import (
	"encoding/json"
	"time"
)

type coreDiagnosticEvent struct {
	Timestamp   int64  `json:"timestamp"`
	Level       string `json:"level"`
	Subsystem   string `json:"subsystem"`
	OperationID string `json:"operationId,omitempty"`
	WorkerID    int    `json:"workerId,omitempty"`
	Stage       string `json:"stage,omitempty"`
	Result      string `json:"result,omitempty"`
	DurationMs  int64  `json:"durationMs,omitempty"`
	Message     string `json:"message,omitempty"`
}

func (c *Core) emitDiagnostic(level, subsystem, stage, result, message string, workerID int, duration time.Duration) {
	if c == nil || !c.debugLogging.Load() {
		return
	}
	event := coreDiagnosticEvent{
		Timestamp: time.Now().UnixMilli(), Level: level, Subsystem: subsystem,
		OperationID: c.cfg.OperationID, WorkerID: workerID, Stage: stage,
		Result: result, Message: message,
	}
	if duration > 0 {
		event.DurationMs = duration.Milliseconds()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	c.emit(Event{Type: EventEvent, Name: "diagnostic", Data: string(data)})
}
