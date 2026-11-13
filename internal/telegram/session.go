package telegram

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// EncryptedSessionStorage handles encrypted on-disk storage for MTProto sessions
type EncryptedSessionStorage struct {
	path     string
	key      []byte
	lockFile *os.File
	mu       sync.Mutex
}

func NewEncryptedSessionStorage(dir, filename, secretKey string) (*EncryptedSessionStorage, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}

	// Derive 32-byte AES key using SHA-256
	keyHash := sha256.Sum256([]byte(secretKey))

	return &EncryptedSessionStorage{
		path: filepath.Join(dir, filename),
		key:  keyHash[:],
	}, nil
}

// LockSession ensures only one collector instance can use this session file simultaneously
func (s *EncryptedSessionStorage) LockSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lockPath := s.path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open session lock file: %w", err)
	}

	// Try non-blocking flock
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return fmt.Errorf("session file is already locked by another active collector instance: %w", err)
	}

	s.lockFile = f
	return nil
}

func (s *EncryptedSessionStorage) UnlockSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lockFile != nil {
		_ = syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
		_ = s.lockFile.Close()
		s.lockFile = nil
	}
	return nil
}

func (s *EncryptedSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No session yet
		}
		return nil, fmt.Errorf("read session file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	// Decrypt AES-GCM
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("malformed session data: size smaller than nonce")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt session: %w", err)
	}

	return plaintext, nil
}

func (s *EncryptedSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	// Write securely with 0600 permissions
	return os.WriteFile(s.path, ciphertext, 0600)
}
