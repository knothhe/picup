//go:build !darwin && !linux

package r2oauth

import "context"

func lockMetadata(context.Context, string) (func(), error) { return nil, ErrSecretUnavailable }
