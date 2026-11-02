//go:build !darwin

package selection

import "context"

func FinderFrontmost() bool { return false }

func Read(context.Context) ([]string, error) { return nil, ErrNoImages }
