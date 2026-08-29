package fleet

import "errors"

// ErrInvalidRequest marks a failure caused by what the caller asked for rather
// than by the fleet, so the API can answer 400 instead of 500.
var ErrInvalidRequest = errors.New("invalid request")
