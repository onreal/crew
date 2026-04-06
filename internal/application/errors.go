package application

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidState  = errors.New("invalid state")
	ErrAlreadyExists = errors.New("already exists")
	ErrPrecondition  = errors.New("precondition failed")
	ErrDisabled      = errors.New("disabled")
)

type NotFoundError struct {
	Entity string
	ID     string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("%s %q: %v", e.Entity, e.ID, ErrNotFound)
}

func (e NotFoundError) Unwrap() error {
	return ErrNotFound
}
