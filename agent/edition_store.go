//go:build store

package main

// The CardputerZero Store edition: never runs anything as root on its own.
// Privileged actions ask the user for their password, like the system Store.
const edition = "store"
