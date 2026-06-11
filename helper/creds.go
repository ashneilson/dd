package helper

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gravypower/dd"
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

// sanitizeForFilename replaces any characters that are not safe in a filename with '_'.
func sanitizeForFilename(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}

// CredsPathForBSID returns the on-disk credentials path for a hub identified by its
// base station ID. The bsid is stable hardware identity, so the credentials file name
// does not change even if the user later edits the MQTT prefix or hub name.
func CredsPathForBSID(dir, bsid string) string {
	return filepath.Join(dir, fmt.Sprintf("dd-credentials-%s.json", sanitizeForFilename(bsid)))
}

// EnsureHubCredentials loads existing credentials for the hub reachable at host, or
// registers new ones using the share code/password if none exist yet.
//
// The hub's stable base station ID (bsid) is fetched directly from the hardware (an
// unauthenticated SDK call that only needs the host) and used as the credentials file
// key. This keeps each hub's credentials stable across configuration changes and lets
// us decide whether registration is required before consuming the one-time share code.
func EnsureHubCredentials(dir, host, shareCode, password string) (*ddapi.RegisterResponse, string, error) {
	infoConn := &dd.Conn{Host: host}
	info, err := ddapi.FetchBasicInfo(infoConn)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach hub at %s to determine its ID: %w", host, err)
	}
	if info.BaseStation == "" {
		return nil, "", fmt.Errorf("hub at %s did not report a base station ID", host)
	}

	path := CredsPathForBSID(dir, info.BaseStation)

	switch _, statErr := os.Stat(path); {
	case statErr == nil:
		// Credentials already exist for this hub; load and reuse them.
		creds, err := LoadCreds(path)
		if err != nil {
			return nil, "", fmt.Errorf("load existing credentials %s: %w", path, err)
		}
		return creds, path, nil
	case !os.IsNotExist(statErr):
		return nil, "", fmt.Errorf("stat credentials %s: %w", path, statErr)
	}

	// No credentials yet: a share code is required to register this hub.
	if shareCode == "" {
		return nil, "", fmt.Errorf("no stored credentials for hub at %s (bsid %s) and no share code provided; add a share code in the configuration to register", host, info.BaseStation)
	}

	creds, err := ddapi.Register(shareCode, password, "API")
	if err != nil {
		return nil, "", fmt.Errorf("register hub at %s: %w", host, err)
	}
	if err := SaveCreds(path, creds); err != nil {
		return nil, "", fmt.Errorf("save credentials %s: %w", path, err)
	}

	logrus.WithFields(logrus.Fields{"path": path, "bsid": info.BaseStation}).Info("Registered new hub credentials")
	return creds, path, nil
}
