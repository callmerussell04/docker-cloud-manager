package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"
)

type BuildParams struct {
	Tag         string
	MemoryBytes int64
	CPUQuota    int64
}

type Adapter struct {
	cli *client.Client
}

func NewAdapter() (*Adapter, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Adapter{cli: cli}, nil
}

// TODO: look into this
func (a *Adapter) BuildImage(ctx context.Context, tarStream io.Reader, params BuildParams) (io.ReadCloser, error) {
	opts := build.ImageBuildOptions{
		Tags:        []string{params.Tag},
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
		Memory:      params.MemoryBytes,
		MemorySwap:  params.MemoryBytes * 2,
		CPUQuota:    params.CPUQuota,
		CPUPeriod:   100000,
	}

	resp, err := a.cli.ImageBuild(ctx, tarStream, opts)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (a *Adapter) InspectImage(ctx context.Context, imageTag string) (int64, error) {
	img, err := a.cli.ImageInspect(ctx, imageTag)
	if err != nil {
		return 0, err
	}
	return img.Size, nil
}
