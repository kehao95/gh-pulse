package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"sync"
)

var warnMissingSecretOnce sync.Once

func verifyWebhookSignature(body []byte, headerSignature string, secret string, logger *log.Logger) (bool, error) {
	if strings.TrimSpace(secret) == "" {
		warnMissingSecretOnce.Do(func() {
			logger.Printf("webhook signature verification disabled: GH_PULSE_WEBHOOK_SECRET is not set")
		})
		return true, nil
	}

	if headerSignature == "" {
		return false, errors.New("missing X-Hub-Signature-256 header")
	}

	const prefix = "sha256="
	if !strings.HasPrefix(headerSignature, prefix) {
		return false, errors.New("invalid signature prefix")
	}

	hexSig := strings.TrimPrefix(headerSignature, prefix)
	provided, err := hex.DecodeString(hexSig)
	if err != nil {
		return false, errors.New("invalid signature encoding")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, provided) {
		return false, errors.New("signature mismatch")
	}

	return true, nil
}
