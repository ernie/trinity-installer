package frame

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

func keyComment() string {
	if host, err := os.Hostname(); err == nil && host != "" {
		return "trinity-installer@" + host
	}
	return "trinity-installer"
}

// LoadOrCreateKey keeps one RSA key per PC; the devkit service accepts only ssh-rsa.
func LoadOrCreateKey(dir string) (ssh.Signer, string, error) {
	path := filepath.Join(dir, "id_rsa")
	pemBytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err := rsa.GenerateKey(rand.Reader, 3072)
		if err != nil {
			return nil, "", err
		}
		pemBytes = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + keyComment()
	os.WriteFile(filepath.Join(dir, "id_rsa.pub"), []byte(line+"\n"), 0o644)
	return signer, line, nil
}
