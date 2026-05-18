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
	assert.Contains(t, err.Error(), "KEY MISMATCH",
		"mismatched keys must surface as a loud security warning, not a generic error")
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
