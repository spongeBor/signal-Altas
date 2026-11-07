package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"path/filepath"
	"signalatlas/api/internal/domain"
	"sync"
	"time"
)

// remote is a pooled SSH connection. Collection runs every thirty seconds, and
// dialling, key exchange and password auth cost more on the router than the
// read-only commands themselves, so the client is kept open between cycles and
// only rebuilt when a session actually fails.
type remote struct {
	mu         sync.Mutex
	client     *ssh.Client
	key        string
	profile    domain.Profile
	secret     string
	knownHosts string
}

var (
	poolMu sync.Mutex
	pool   = map[string]*remote{}
)

// poolKey identifies one device account. The secret digest is part of it so a
// changed password never reuses a session authenticated with the old one.
func poolKey(p domain.Profile, password string) string {
	sum := sha256.Sum256([]byte(password))
	return p.ID + "|" + p.Host + "|" + p.Username + "|" + hex.EncodeToString(sum[:8])
}

func connectSSH(ctx context.Context, p domain.Profile, password, knownHosts string) (*remote, error) {
	if knownHosts == "" {
		home, _ := os.UserHomeDir()
		knownHosts = filepath.Join(home, ".ssh", "known_hosts")
	}
	key := poolKey(p, password)
	poolMu.Lock()
	if r, ok := pool[key]; ok && r.knownHosts == knownHosts {
		poolMu.Unlock()
		return r, nil
	}
	poolMu.Unlock()
	// Validate the host key file and the password before dialling so the
	// existing error messages stay the same on a cold connection.
	if _, err := knownhosts.New(knownHosts); err != nil {
		return nil, errors.New("无法读取SSH known_hosts，请先在终端验证设备主机指纹")
	}
	if password == "" {
		return nil, errors.New("需要输入设备管理密码（仅保留在运行内存）")
	}
	r := &remote{key: key, profile: p, secret: password, knownHosts: knownHosts}
	client, err := dialSSH(ctx, p, password, knownHosts)
	if err != nil {
		return nil, err
	}
	r.client = client
	poolMu.Lock()
	previous := pool[key]
	pool[key] = r
	poolMu.Unlock()
	if previous != nil {
		previous.drop()
	}
	return r, nil
}

// UnknownHostKey is returned while a fresh host key waits for the operator to
// approve it in the UI. The key itself is kept in the pending list.
type UnknownHostKey struct {
	Host        string
	Fingerprint string
}

func (e *UnknownHostKey) Error() string { return "SSH 主机指纹待确认：" + e.Fingerprint }

type pendingKey struct {
	Host        string
	Fingerprint string
	KeyType     string
	Path        string
	At          time.Time
	Key         ssh.PublicKey
}

var pendingKeys = struct {
	sync.Mutex
	byHost map[string]pendingKey
}{byHost: map[string]pendingKey{}}

// PendingHostKeys lists fingerprints waiting for approval, newest hosts last.
func PendingHostKeys() []pendingKey {
	pendingKeys.Lock()
	defer pendingKeys.Unlock()
	out := make([]pendingKey, 0, len(pendingKeys.byHost))
	for _, k := range pendingKeys.byHost {
		out = append(out, k)
	}
	return out
}

// PendingHostKeyView is the JSON-safe projection used by the API.
type PendingHostKeyView struct {
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"key_type"`
	SeenAt      string `json:"seen_at"`
}

func PendingHostKeyViews() []PendingHostKeyView {
	out := []PendingHostKeyView{}
	for _, k := range PendingHostKeys() {
		out = append(out, PendingHostKeyView{Host: k.Host, Fingerprint: k.Fingerprint, KeyType: k.KeyType, SeenAt: k.At.UTC().Format(time.RFC3339)})
	}
	return out
}

func rememberPending(host, path string, key ssh.PublicKey) {
	pendingKeys.Lock()
	defer pendingKeys.Unlock()
	pendingKeys.byHost[host] = pendingKey{Host: host, Fingerprint: ssh.FingerprintSHA256(key), KeyType: key.Type(),
		Path: path, At: time.Now(), Key: key}
}

// TrustHostKey appends the remembered key to known_hosts so the next dial
// succeeds. It refuses when no key has been seen for that host.
func TrustHostKey(host, path string) error {
	pendingKeys.Lock()
	k, ok := pendingKeys.byHost[host]
	defer pendingKeys.Unlock()
	if !ok {
		return errors.New("没有待确认的主机指纹")
	}
	if path == "" {
		path = k.Path
	}
	if path == "" {
		return errors.New("未配置 known_hosts 路径")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("无法创建 known_hosts 目录")
	}
	line := knownhosts.Line([]string{host}, k.Key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("无法写入 known_hosts")
	}
	if _, err = f.Write([]byte(line + "\n")); err != nil {
		f.Close()
		return errors.New("无法写入 known_hosts")
	}
	if err = f.Close(); err != nil {
		return errors.New("无法写入 known_hosts")
	}
	delete(pendingKeys.byHost, host)
	return nil
}

// hostKeyChecker wraps the known_hosts callback: an unknown host records the
// presented key and fails the dial until an operator approves it.
func hostKeyChecker(path string) (ssh.HostKeyCallback, error) {
	check, err := knownhosts.New(path)
	if err != nil {
		return nil, errors.New("无法读取SSH known_hosts，请先在终端验证设备主机指纹")
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			host := remoteAddrHost(remote)
			rememberPending(host, path, key)
			return &UnknownHostKey{Host: host, Fingerprint: ssh.FingerprintSHA256(key)}
		}
		return err
	}, nil
}

func remoteAddrHost(remote net.Addr) string {
	if remote == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(remote.String()); err == nil {
		return host
	}
	return remote.String()
}

func dialSSH(ctx context.Context, p domain.Profile, password, knownHosts string) (*ssh.Client, error) {
	check, err := hostKeyChecker(knownHosts)
	if err != nil {
		return nil, errors.New("无法读取SSH known_hosts，请先在终端验证设备主机指纹")
	}
	config := &ssh.ClientConfig{User: p.Username, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: check, Timeout: 8 * time.Second}
	addr := net.JoinHostPort(p.Host, "22")
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, errors.New("SSH网络连接失败或超时")
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	cc, ch, req, err := ssh.NewClientConn(conn, addr, config)
	// Go's default algorithm order can select a different valid server key than
	// the one previously approved by OpenSSH. Retry only approved key types;
	// the same known_hosts callback still verifies the exact public key.
	var mismatch *knownhosts.KeyError
	if errors.As(err, &mismatch) && len(mismatch.Want) > 0 {
		conn.Close()
		config.HostKeyAlgorithms = approvedAlgorithms(mismatch.Want)
		conn, err = (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, errors.New("SSH网络连接失败或超时")
		}
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		cc, ch, req, err = ssh.NewClientConn(conn, addr, config)
	}
	if err != nil {
		conn.Close()
		var unknown *UnknownHostKey
		if errors.As(err, &unknown) {
			return nil, errors.New("SSH 主机指纹待确认，请在「采集与设置」点信任后重试")
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			return nil, errors.New("SSH主机指纹未知或发生变化，已停止连接，请在终端核对")
		}
		return nil, errors.New("SSH登录失败，请检查账号与密码")
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(cc, ch, req), nil
}

func approvedAlgorithms(keys []knownhosts.KnownKey) []string {
	list := []string{}
	seen := map[string]bool{}
	for _, k := range keys {
		algos := []string{k.Key.Type()}
		if k.Key.Type() == ssh.KeyAlgoRSA {
			algos = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
		}
		for _, a := range algos {
			if !seen[a] {
				list = append(list, a)
				seen[a] = true
			}
		}
	}
	return list
}

// drop closes the current client and clears it so the next run dials again.
// Callers already hold r.mu.
func (r *remote) drop() {
	if r.client != nil {
		_ = r.client.Close()
		r.client = nil
	}
}

// close is deliberately a no-op: connections are pooled across collection
// cycles and only dropped when a session fails or the password changes.
func (r *remote) close() {}

func (r *remote) ensure(ctx context.Context) (*ssh.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	client, err := dialSSH(ctx, r.profile, r.secret, r.knownHosts)
	if err != nil {
		return nil, err
	}
	r.client = client
	return client, nil
}

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("设备输出超过采集上限")
	}
	return b.Buffer.Write(p)
}

func (r *remote) run(ctx context.Context, command string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client, err := r.ensure(ctx)
	if err != nil {
		return "", err
	}
	out, err := execSession(ctx, client, command)
	if err == nil {
		return out, nil
	}
	// A router reboot, an idle TCP timeout or a restarted dropbear leaves the
	// pooled session dead; rebuild it once and repeat the read-only command.
	r.drop()
	if ctx.Err() != nil {
		return "", err
	}
	if client, err = r.ensure(ctx); err != nil {
		return "", err
	}
	return execSession(ctx, client, command)
}

func execSession(ctx context.Context, client *ssh.Client, command string) (string, error) {
	s, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	out := &cappedBuffer{max: 1 << 20}
	s.Stdout = out
	s.Stderr = &cappedBuffer{max: 8192}
	done := make(chan error, 1)
	go func() { done <- s.Run(command) }()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", errors.New("设备读取超时")
	case err := <-done:
		// Optional firmware commands can be absent; preserve successful stdout.
		if err != nil && out.Len() == 0 {
			var exit *ssh.ExitError
			if !errors.As(err, &exit) {
				return "", errors.New("SSH会话读取失败")
			}
		}
		return stringsClean(out.String()), nil
	}
}

func stringsClean(s string) string { return string(bytes.ReplaceAll([]byte(s), []byte{'\r'}, nil)) }
