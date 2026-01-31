package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type ProcessUpdateMessagePayload struct {
	UpdateID uuid.UUID `json:"update_id"`
}

func (c *Connection) PublishProcessUpdateMessage(
	ctx context.Context,
	updateID uuid.UUID,
) error {
	data, err := json.Marshal(ProcessUpdateMessagePayload{UpdateID: updateID})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	return c.nc.Publish(processUpdateSubjectName, data)
}

func ParseProcessUpdateMessage(data []byte) (*ProcessUpdateMessagePayload, error) {
	var payload ProcessUpdateMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
