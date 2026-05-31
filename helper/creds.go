package helper

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"

	ddapi "github.com/gravypower/dd/api"
	"github.com/sirupsen/logrus"
)

// We hash this phrase to get a 32-byte key for AES-256 for at-rest encryption.
// While a static key implies security through obscurity, it's vastly superior to plaintext JSON on disk.
const staticEncryptionKeySeed = "SmartDoor-HA-Addon-Static-Key-2026"

func getEncryptionKey() []byte {
	hash := sha256.Sum256([]byte(staticEncryptionKeySeed))
	return hash[:]
}

func encryptData(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(getEncryptionKey())
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func decryptData(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(getEncryptionKey())
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aesgcm.NonceSize() {
		return nil, errors.New("ciphertext too short to contain nonce")
	}
	nonce, ciphertext := ciphertext[:aesgcm.NonceSize()], ciphertext[aesgcm.NonceSize():]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// SaveCreds encrypts and saves a RegisterResponse to disk.
func SaveCreds(p string, creds *ddapi.RegisterResponse) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	encryptedData, err := encryptData(data)
	if err != nil {
		return err
	}
	return os.WriteFile(p, encryptedData, 0600)
}

// LoadCreds loads a RegisterResponse from disk. Supports seamless migration from plaintext.
func LoadCreds(p string) (*ddapi.RegisterResponse, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, errors.New("credentials file is empty")
	}

	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var creds ddapi.RegisterResponse
	
	// Check if this is a legacy plaintext JSON file
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		// Attempt to parse as plaintext JSON
		if err := json.Unmarshal(data, &creds); err != nil {
			return nil, err
		}
		
		// If successful, re-save it as encrypted to perform seamless migration
		logrus.Infof("Migrating plaintext credentials file %s to encrypted format", p)
		if err := SaveCreds(p, &creds); err != nil {
			logrus.WithError(err).Warn("Failed to migrate credentials to encrypted format, but will proceed")
		}
		
		return &creds, nil
	}

	// Assuming encrypted format
	plaintext, err := decryptData(data)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(plaintext, &creds)
	return &creds, err
}
