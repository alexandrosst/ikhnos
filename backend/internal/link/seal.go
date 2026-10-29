package link

// Seal and Open are the sealing the link credentials travel under (AES-256-GCM, key derived from a secret and a label, bound to additional
// data), for the other packages that move the same credentials between pods. The label names what is sealed, so a blob made for one purpose
// does not open for another, and aad is what it is bound to (the signature of the request it answers, or of the request that carries it).

// Seal encrypts plain. The result is nonce || ciphertext.
func Seal(secret []byte, label, aad string, plain []byte) ([]byte, error) {
	return seal(secret, label, aad, plain)
}

// Open is the inverse of Seal. The error says nothing about why it failed.
func Open(secret []byte, label, aad string, sealed []byte) ([]byte, error) {
	return open(secret, label, aad, sealed)
}
