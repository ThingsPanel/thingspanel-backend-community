package sdk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// DeploymentSettings is supplied only by the platform administrator. The secret is
// accepted on writes and never returned by the management endpoint.
type DeploymentSettings struct {
	Address string `json:"address"`
	Secret  string `json:"secret,omitempty"`
	Version int64  `json:"version"`
}

type settingsServer struct {
	mu        sync.RWMutex
	current   http.Handler
	active    DeploymentSettings
	build     func(DeploymentSettings) (http.Handler, error)
	tokenHash [32]byte
	cipher    cipher.AEAD
	file      string
}

// WithDeploymentSettings retains the environment-defined provider until the first
// successfully persisted update. An unusable saved configuration fails startup.
func WithDeploymentSettings(base http.Handler, token, file string, initial DeploymentSettings, build func(DeploymentSettings) (http.Handler, error)) (http.Handler, error) {
	if file == "" {
		return base, nil
	}
	if len(token) < 32 {
		return nil, errors.New("configuration key is too short")
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("ThingsPanel provider settings v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	s := &settingsServer{active: initial, current: base, tokenHash: sha256.Sum256([]byte("Bearer " + token)), file: file, cipher: aead, build: build}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil || len(data) < aead.NonceSize() {
		return nil, errors.New("cannot read saved provider settings")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil || decodeSavedSettings(plain, &s.active) != nil {
		return nil, errors.New("invalid saved provider settings")
	}
	s.current, err = build(s.active)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Decode into an empty value: an omitted saved secret must not inherit an environment secret.
func decodeSavedSettings(raw []byte, dest *DeploymentSettings) error {
	var saved DeploymentSettings
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	*dest = saved
	return nil
}
func (s *settingsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/deployment-settings" {
		s.mu.RLock()
		handler := s.current
		s.mu.RUnlock()
		handler.ServeHTTP(w, r)
		return
	}
	auth := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if subtle.ConstantTimeCompare(auth[:], s.tokenHash[:]) != 1 {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		var next DeploymentSettings
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		dec.DisallowUnknownFields()
		if dec.Decode(&next) != nil || dec.Decode(&struct{}{}) != io.EOF || next.Address == "" {
			w.WriteHeader(400)
			return
		}
		if next.Version != s.active.Version {
			w.WriteHeader(409)
			return
		}
		if next.Secret == "" && next.Address == s.active.Address {
			next.Secret = s.active.Secret
		}
		provider, err := s.build(next)
		if err != nil {
			w.WriteHeader(422)
			return
		}
		next.Version++
		if err = s.save(next); err != nil {
			w.WriteHeader(503)
			return
		}
		s.current = provider
		s.active = next
	case http.MethodGet:
	default:
		w.WriteHeader(405)
		return
	}
	json.NewEncoder(w).Encode(struct {
		Address   string `json:"address"`
		Version   int64  `json:"version"`
		HasSecret bool   `json:"hasSecret"`
	}{s.active.Address, s.active.Version, s.active.Secret != ""})
}
func (s *settingsServer) save(v DeploymentSettings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	blob := s.cipher.Seal(nonce, nonce, raw, nil)
	f, err := os.CreateTemp(filepath.Dir(s.file), ".provider-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(blob); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), s.file)
}
