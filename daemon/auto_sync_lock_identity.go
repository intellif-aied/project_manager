package main

import (
	"crypto/sha256"
	"encoding/hex"
)

func autoSyncLockIdentity(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:16])
}
