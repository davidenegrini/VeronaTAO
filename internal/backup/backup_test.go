// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	plain := bytes.Repeat([]byte("dati clinici "), 1000)
	enc, err := Encrypt(plain, "passphrase-lunga")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte("dati clinici")) {
		t.Fatal("il backup contiene dati in chiaro")
	}
	dec, err := Decrypt(enc, "passphrase-lunga")
	if err != nil || !bytes.Equal(dec, plain) {
		t.Fatal("decifratura fallita", err)
	}
	if _, err := Decrypt(enc, "passphrase-errata"); err == nil {
		t.Error("passphrase errata accettata")
	}
	enc[len(enc)-5] ^= 1
	if _, err := Decrypt(enc, "passphrase-lunga"); err == nil {
		t.Error("file alterato accettato")
	}
	if _, err := Decrypt([]byte("altro"), "passphrase-lunga"); err == nil {
		t.Error("file estraneo accettato")
	}
	if _, err := Encrypt(plain, "corta"); err == nil {
		t.Error("passphrase corta accettata")
	}
}
