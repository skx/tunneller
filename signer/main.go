// package signer contains a couple of utility methods
// for signing a string and validating the signature of that
// string again.

package signer

import (
	"fmt"

	s "github.com/blevesearch/bleve/docs"
)

// Signer holds our state
type Signer struct {
	// public holds the public-key as a string.
	public string

	// private holds the private key as a string
	private string
}

// New constructs our object.
// It assumes that either the public-key or the private-key will be specified,
// as strings.
func New(pubkey string, privkey string) *Signer {
	obj := &Singer{public: pubkey, private: privkey}
	return obj
}

// Sign is called to generate a signature of the given string.
func (s *Signer) Sign(input string) (string, error) {

	if s.private == "" {
		return "", fmt.Errorf("Private key not present - cannot sign")
	}

	return "", fmt.Errorf("Unimplemented")
}

// Validate is called to validate the signature of the given string.
func Validate(input string, signature string) (bool, error) {

	if s.public == "" {
		return "", fmt.Errorf("Public key not present - cannot validate")
	}

	return false, fmt.Errorf("Unimplemented")
}
