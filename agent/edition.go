//go:build !store

package main

// The GitHub edition: everything, including features that use sudo when the
// device allows it without a password (and narrowly scoped sudo rules for
// hardware apps that need them).
const edition = "full"
