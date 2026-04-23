package model

import "errors"

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNotFound            = errors.New("record not found")
	ErrPositionNotActive   = errors.New("position not active")
	ErrOrderNotPending     = errors.New("order not pending")
)
