package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestBuildHostKey_envVarWins(t *testing.T) {
	pub := genHostKey(t)
	t.Setenv("SSHT_TEST_HOSTKEY", base64.StdEncoding.EncodeToString(pub.Marshal()))

	cb, err := buildHostKeyCallback("/nonexistent/known_hosts")
	require.NoError(t, err)
	require.NotNil(t, cb)

	// Callback should accept the pinned key without consulting the file.
	require.NoError(t, cb("host:22", nil, pub))
}

func TestBuildHostKey_knownHostsHit(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	pub := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")

	line := "hudson " + pub.Type() + " " + base64.StdEncoding.EncodeToString(pub.Marshal()) + "\n"
	require.NoError(t, os.WriteFile(khPath, []byte(line), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	// Simulate the SSH client invoking the callback with the matching key.
	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	require.NoError(t, cb("hudson:22", addr, pub))
}

func TestBuildHostKey_knownHostsMissing_returnsClearError(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	pub := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(""), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	err = cb("hudson:22", addr, pub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hudson")
	assert.Contains(t, err.Error(), "ssh-keyscan", "error should tell user how to fix it")
	require.ErrorIs(t, err, ErrHostNotInKnownHosts)
}

func TestBuildHostKey_keyMismatch_returnsSecurityWarning(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	saved := genHostKey(t)
	presented := genHostKey(t) // different key for the same host

	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	line := "hudson " + saved.Type() + " " + base64.StdEncoding.EncodeToString(saved.Marshal()) + "\n"
	require.NoError(t, os.WriteFile(khPath, []byte(line), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	addr := &fakeAddr{net: "tcp", str: "1.2.3.4:22"}
	err = cb("hudson:22", addr, presented)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrKeyMismatch,
		"mismatched keys must wrap ErrKeyMismatch so classifyError routes to exit 2")
	// Keep a soft assertion on the string for human readability:
	assert.Contains(t, err.Error(), "KEY MISMATCH",
		"user-facing message should still surface the security warning prominently")
}

func TestBuildHostKey_nonStandardPort_emitsCorrectScanCommand(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	pub := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	require.NoError(t, os.WriteFile(khPath, []byte(""), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)

	// Hostname:port for a non-standard port — should emit -p flag and
	// [host]:port bracket form in the keygen advice.
	addr := &fakeAddr{net: "tcp", str: "203.0.113.10:2200"}
	err = cb("203.0.113.10:2200", addr, pub)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrHostNotInKnownHosts)
	assert.Contains(t, err.Error(), "ssh-keyscan -H -p 2200 203.0.113.10",
		"ssh-keyscan must include -p PORT for non-standard ports")
	assert.Contains(t, err.Error(), "[203.0.113.10]:2200",
		"missing-host advice must reference [host]:port form for non-standard ports")
}

func TestBuildHostKey_keyMismatch_nonStandardPort_emitsCorrectGenCmd(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	saved := genHostKey(t)
	presented := genHostKey(t)
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	// Save entry under bracket form for non-22 port.
	line := "[203.0.113.10]:2200 " + saved.Type() + " " +
		base64.StdEncoding.EncodeToString(saved.Marshal()) + "\n"
	require.NoError(t, os.WriteFile(khPath, []byte(line), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	require.NoError(t, err)
	addr := &fakeAddr{net: "tcp", str: "203.0.113.10:2200"}
	err = cb("203.0.113.10:2200", addr, presented)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrKeyMismatch)
	assert.Contains(t, err.Error(), "[203.0.113.10]:2200",
		"ssh-keygen -R target must use [host]:port form for non-standard ports")
	assert.Contains(t, err.Error(), "ssh-keyscan -H -p 2200 203.0.113.10",
		"re-add advice must include -p PORT for non-standard ports")
}

func TestBuildHostKey_malformedFile_returnsClearError(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	// Garbage that knownhosts.New cannot parse.
	require.NoError(t, os.WriteFile(khPath, []byte("\xff\xfe binary garbage \n"), 0o600))

	cb, err := buildHostKeyCallback(khPath)
	// knownhosts.New is permissive about junk lines, so this may
	// succeed and return a callback that just doesn't match anything.
	// Either outcome is acceptable; what we don't want is a panic.
	if err == nil && cb == nil {
		t.Fatal("returned (nil, nil)")
	}
	if err != nil {
		assert.Contains(t, err.Error(), "known_hosts",
			"error should mention known_hosts for context")
	}
}

func TestBuildHostKey_noFile(t *testing.T) {
	t.Setenv("SSHT_TEST_HOSTKEY", "")
	cb, err := buildHostKeyCallback("/no/such/file/known_hosts")
	require.Error(t, err)
	require.Nil(t, cb)
	assert.Contains(t, err.Error(), "known_hosts")
}

// ----- helpers -----

func genHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)
	return signer.PublicKey()
}

type fakeAddr struct{ net, str string }

func (f *fakeAddr) Network() string { return f.net }
func (f *fakeAddr) String() string  { return f.str }
