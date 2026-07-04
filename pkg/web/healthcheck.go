package web

import (
	"context"
	"fmt"

	"github.com/nint8835/planespotter/pkg/tar1090"
)

// Healthchecker checks whether dependencies required by the API are healthy.
type Healthchecker interface {
	CheckHealth(ctx context.Context) error
}

type tar1090Healthchecker struct {
	client *tar1090.Client
}

func newTar1090Healthchecker(tar1090URL string) (Healthchecker, error) {
	client, err := tar1090.NewClient(tar1090URL)
	if err != nil {
		return nil, fmt.Errorf("create tar1090 client: %w", err)
	}

	return tar1090Healthchecker{client: client}, nil
}

func (h tar1090Healthchecker) CheckHealth(ctx context.Context) error {
	if _, err := h.client.FetchAircraft(ctx); err != nil {
		return fmt.Errorf("fetch tar1090 aircraft: %w", err)
	}

	return nil
}
