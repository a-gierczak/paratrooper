package codepush

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func ParseDeploymentKey(
	deploymentKey string,
) (projectID uuid.UUID, platform, channel string, err error) {
	decoded, err := url.QueryUnescape(deploymentKey)
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("failed to decode deployment key: %w", err)
	}

	parts := strings.SplitN(decoded, "/", 3)
	if len(parts) != 3 {
		return uuid.Nil, "", "", fmt.Errorf(
			"invalid deployment key format, expected projectID/platform/channel, got: %s",
			decoded,
		)
	}

	projectID, err = uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("invalid project id: %w", err)
	}

	return projectID, parts[1], parts[2], nil
}
