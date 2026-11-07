package collector

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"testing"
	"time"
)

func TestApprovedSSHAlgorithmPrefersRecordedKey(t *testing.T) {
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewPublicKey(public)
	got := approvedAlgorithms([]knownhosts.KnownKey{{Key: key}, {Key: key}})
	if len(got) != 1 || got[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("unapproved or duplicate algorithm: %v", got)
	}
}
func TestESXiPinIsRequiredAndValidDatesChecked(t *testing.T) {
	cert := &x509.Certificate{Raw: []byte("approved-public-certificate"), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	hash := sha256.Sum256(cert.Raw)
	cfg, err := pinnedTLSConfig(hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VerifyConnection == nil {
		t.Fatal("pin verification absent")
	}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	if err = cfg.VerifyConnection(cs); err != nil {
		t.Fatal(err)
	}
	altered := *cert
	altered.Raw = []byte("different-certificate")
	if cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{&altered}}) == nil {
		t.Fatal("wrong certificate accepted")
	}
	expired := *cert
	expired.NotAfter = time.Now().Add(-time.Minute)
	if cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{&expired}}) == nil {
		t.Fatal("expired certificate accepted")
	}
	if cfg.VerifyConnection(tls.ConnectionState{}) == nil {
		t.Fatal("missing certificate accepted")
	}
}
