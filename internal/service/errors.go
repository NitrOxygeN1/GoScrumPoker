package service

import "errors"

// ErrInvalidInput is returned when required identifiers or names are missing.
var ErrInvalidInput = errors.New("invalid input")
