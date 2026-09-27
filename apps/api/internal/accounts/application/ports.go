package application

import "context"

type OwnerVerifier interface {
	Verify(context.Context, string) (string, error)
}
