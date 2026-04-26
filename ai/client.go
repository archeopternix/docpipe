package ai

import "context"

type Client interface {
	Generate(ctx context.Context, instructions, input string) (string, error)
}
