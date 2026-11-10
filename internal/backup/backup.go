// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backup cifra e decifra le copie dell'archivio con una passphrase.
// Formato: intestazione, sale (16 byte), nonce (12 byte), dati AES-256-GCM.
// La chiave è derivata dalla passphrase con Argon2id.
package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/argon2"
)

var magic = []byte("VERONATAO-BACKUP-1\n")

// MinPassphrase è la lunghezza minima accettata per la passphrase.
const MinPassphrase = 10

func key(pass string, salt []byte) []byte {
	return argon2.IDKey([]byte(pass), salt, 3, 64*1024, 2, 32)
}

func Encrypt(plain []byte, pass string) ([]byte, error) {
	if len(pass) < MinPassphrase {
		return nil, errors.New("la passphrase deve avere almeno 10 caratteri")
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key(pass, salt))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, magic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plain, magic), nil
}

func Decrypt(data []byte, pass string) ([]byte, error) {
	if !bytes.HasPrefix(data, magic) || len(data) < len(magic)+28 {
		return nil, errors.New("il file non è un backup di VeronaTAO")
	}
	rest := data[len(magic):]
	salt, nonce, ct := rest[:16], rest[16:28], rest[28:]
	block, err := aes.NewCipher(key(pass, salt))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, magic)
	if err != nil {
		return nil, errors.New("passphrase errata o file danneggiato")
	}
	return plain, nil
}
