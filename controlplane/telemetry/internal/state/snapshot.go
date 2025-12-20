package state

import "encoding/json"

type StateSnapshot struct {
	Metadata StateSnapshotMetadata `json:"metadata"`
	Data     json.RawMessage       `json:"data"`
}

type StateSnapshotMetadata struct {
	Kind      string `json:"kind"`
	Timestamp string `json:"timestamp"`
	Device    string `json:"device"`
}
