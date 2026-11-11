package keystore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oenexa/oenexa/internal/crypto"
	"github.com/oenexa/oenexa/internal/keystore"
)

func TestDecrypt_KDFDoSBombsAndBounds(t *testing.T) {
	tmp := t.TempDir()
	w, err := crypto.NewWallet()
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	basePath := filepath.Join(tmp, "base.json")
	if err := keystore.Encrypt(basePath, "pw", w); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	raw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "oom memory bomb",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["memory"] = uint32(4_000_000_000)
			},
			wantErr: "invalid KDF memory",
		},
		{
			name: "downgrade memory zero",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["memory"] = 0
			},
			wantErr: "invalid KDF memory",
		},
		{
			name: "cpu time bomb",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["time"] = 999999
			},
			wantErr: "invalid KDF time",
		},
		{
			name: "downgrade time zero",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["time"] = 0
			},
			wantErr: "invalid KDF time",
		},
		{
			name: "threads zero",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["threads"] = 0
			},
			wantErr: "invalid KDF threads",
		},
		{
			name: "short salt downgrade",
			mutate: func(m map[string]any) {
				m["kdf_params"].(map[string]any)["salt"] = "AQID" // 3 bytes
			},
			wantErr: "invalid salt length",
		},
		{
			name: "bad iv length",
			mutate: func(m map[string]any) {
				m["cipher_params"].(map[string]any)["iv"] = "AQID"
			},
			wantErr: "invalid IV length",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			tc.mutate(m)
			out, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			p := filepath.Join(tmp, strings.ReplaceAll(tc.name, " ", "_")+".json")
			if err := os.WriteFile(p, out, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			_, err = keystore.Decrypt(p, "pw")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
