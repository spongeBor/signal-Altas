package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// secretStore keeps collector passwords as AES-GCM ciphertext on the private
// data volume so rebuilding the container no longer requires re-entering them.
// The key stays in the deployment host's environment; without it the engine
// keeps passwords in memory only, exactly as before.
type secretStore struct {
	path string
	aead cipher.AEAD
}

func newSecretStore(path, keyHex string) (*secretStore, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, errors.New("SIGNAL_ATLAS_SECRET_KEY 应为64位十六进制")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("无法初始化凭据加密")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("无法初始化凭据加密")
	}
	return &secretStore{path: path, aead: aead}, nil
}

func (s *secretStore) load() (map[string]string, error) {
	out := map[string]string{}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, errors.New("无法读取凭据文件")
	}
	if len(raw) <= s.aead.NonceSize() {
		return nil, errors.New("凭据文件格式无效")
	}
	nonce, sealed := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, errors.New("凭据文件解密失败，请检查SIGNAL_ATLAS_SECRET_KEY")
	}
	defer clear(plain)
	if err = json.Unmarshal(plain, &out); err != nil {
		return nil, errors.New("凭据文件内容无效")
	}
	return out, nil
}

func (s *secretStore) save(secrets map[string]string) error {
	plain, err := json.Marshal(secrets)
	if err != nil {
		return errors.New("凭据序列化失败")
	}
	defer clear(plain)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return errors.New("无法生成凭据随机数")
	}
	blob := s.aead.Seal(nonce, nonce, plain, nil)
	dir := filepath.Dir(s.path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("无法创建凭据目录")
	}
	tmp, err := os.CreateTemp(dir, ".secrets-")
	if err != nil {
		return errors.New("无法写入凭据文件")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return errors.New("无法设置凭据文件权限")
	}
	if _, err = tmp.Write(blob); err != nil {
		tmp.Close()
		return errors.New("无法写入凭据文件")
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return errors.New("无法同步凭据文件")
	}
	if err = tmp.Close(); err != nil {
		return errors.New("无法写入凭据文件")
	}
	if err = os.Rename(name, s.path); err != nil {
		return errors.New("无法保存凭据文件")
	}
	return nil
}
