package toolservicecontract

import (
	"crypto/sha256"
	"encoding/hex"
)

// PrivateBearer separates tool-service authority from relay authentication. A
// tool client never receives the credential needed to impersonate its wrapper.
func PrivateBearer(carrierCredential string) string {
	if carrierCredential == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("fschannel.private-tools.v1\x00" + carrierCredential))
	return hex.EncodeToString(sum[:])
}
