//go:build !unix

package server

import "errors"

// LockPID refuses: this platform has no flock, so the gate cannot be proved.
func LockPID(string) (func(), error) {
	return nil, errors.New("server: para serve needs a unix platform")
}

// PIDAlive reports false: no server runs here.
func PIDAlive(int) bool { return false }
