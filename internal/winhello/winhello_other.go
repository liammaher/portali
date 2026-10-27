//go:build !windows

package winhello

import "context"

// Available reports why Windows Hello can't approve a request: it can't here.
func (Hello) Available() error { return ErrUnsupported }

// Approve never approves here.
func (Hello) Approve(ctx context.Context, window uint64, message string) (bool, error) {
	return false, ErrUnsupported
}
