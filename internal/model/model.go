package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// OperationType represents the kind of change an operation performs.
type OperationType string

const (
	OperationAdd    OperationType = "ADD"
	OperationUpdate OperationType = "UPDATE"
	OperationDelete OperationType = "DELETE"
)

// Operation is the atomic unit of change. It is the only thing written to disk.
type Operation struct {
	OperationID string         `json:"operation_id"`
	ItemID      string         `json:"item_id"`
	DeviceID    string         `json:"device_id"`
	Type        OperationType  `json:"type"`
	Payload     map[string]any `json:"payload"`
	Timestamp   time.Time      `json:"timestamp"`
}

// Item is the derived read model produced by replaying operations.
type Item struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted"`
}

var (
	ErrInvalidOperationType = errors.New("invalid operation type")
	ErrMissingDeviceID      = errors.New("missing device id")
	ErrMissingItemID        = errors.New("missing item id")
)

// NewOperationID returns an ID in the format "<deviceID>:<ulid>".
func NewOperationID(deviceID string) (string, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", ErrMissingDeviceID
	}
	return fmt.Sprintf("%s:%s", deviceID, ulid.Make().String()), nil
}

func (t OperationType) Valid() bool {
	switch t {
	case OperationAdd, OperationUpdate, OperationDelete:
		return true
	default:
		return false
	}
}

// ValidateBasic validates fields common to all operations.
func (o Operation) ValidateBasic() error {
	if strings.TrimSpace(o.OperationID) == "" {
		return errors.New("missing operation id")
	}
	if strings.TrimSpace(o.ItemID) == "" {
		return ErrMissingItemID
	}
	if strings.TrimSpace(o.DeviceID) == "" {
		return ErrMissingDeviceID
	}
	if !o.Type.Valid() {
		return ErrInvalidOperationType
	}
	if o.Payload == nil {
		return errors.New("missing payload")
	}
	return nil
}

// NewAddOperation creates an ADD operation.
func NewAddOperation(deviceID, itemID, content string, now time.Time) (Operation, error) {
	opID, err := NewOperationID(deviceID)
	if err != nil {
		return Operation{}, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Operation{}, ErrMissingItemID
	}
	return Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    strings.TrimSpace(deviceID),
		Type:        OperationAdd,
		Payload:     map[string]any{"content": content},
		Timestamp:   now,
	}, nil
}

// NewUpdateOperation creates an UPDATE operation.
func NewUpdateOperation(deviceID, itemID, content string, now time.Time) (Operation, error) {
	opID, err := NewOperationID(deviceID)
	if err != nil {
		return Operation{}, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Operation{}, ErrMissingItemID
	}
	return Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    strings.TrimSpace(deviceID),
		Type:        OperationUpdate,
		Payload:     map[string]any{"content": content},
		Timestamp:   now,
	}, nil
}

// NewDeleteOperation creates a DELETE operation.
func NewDeleteOperation(deviceID, itemID string, now time.Time) (Operation, error) {
	opID, err := NewOperationID(deviceID)
	if err != nil {
		return Operation{}, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Operation{}, ErrMissingItemID
	}
	return Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    strings.TrimSpace(deviceID),
		Type:        OperationDelete,
		Payload:     map[string]any{"deleted": true},
		Timestamp:   now,
	}, nil
}
