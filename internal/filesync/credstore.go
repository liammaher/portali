package filesync

import (
	"fmt"
	"os/user"
	"runtime"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/wincred"
)

// SystemStore is where this computer keeps sign-ins: the macOS Keychain, or
// Windows Credential Manager. Elsewhere it is the Keychain store, which says
// it needs a Mac.
func SystemStore() SecretStore {
	if runtime.GOOS == "windows" {
		return CredentialStore{}
	}
	return KeychainStore{}
}

// CredentialStore is Windows Credential Manager: generic credentials of this
// user, chunked as in the Keychain (a credential holds 2.5 KB).
type CredentialStore struct{}

type credentialItems struct{ user string }

func (credentialItems) where() string { return "Credential Manager" }

func (c credentialItems) loadItem(service string) (string, error) {
	secret, _, err := wincred.Read(service)
	if err == wincred.ErrNotFound {
		return "", nil
	}
	return secret, err
}

func (c credentialItems) saveItem(service, value string) error {
	return wincred.Write(service, c.user, value)
}

func (c credentialItems) deleteItem(service string) error {
	return wincred.Delete(service)
}

func (CredentialStore) items(service string) (credentialItems, error) {
	if runtime.GOOS != "windows" {
		return credentialItems{}, wincred.ErrUnsupported
	}
	if !reService.MatchString(service) {
		return credentialItems{}, fmt.Errorf("invalid credential name %q", service)
	}
	u, err := user.Current()
	if err != nil {
		return credentialItems{}, err
	}
	// DOMAIN\name: the name, as Windows shows the signed-in user.
	return credentialItems{user: u.Username[strings.LastIndex(u.Username, `\`)+1:]}, nil
}

func (c CredentialStore) Load(service string) (string, error) {
	it, err := c.items(service)
	if err != nil {
		return "", err
	}
	return loadChunked(it, service)
}

func (c CredentialStore) Save(service, secret string) error {
	it, err := c.items(service)
	if err != nil {
		return err
	}
	return saveChunked(it, service, secret)
}

func (c CredentialStore) Delete(service string) error {
	it, err := c.items(service)
	if err != nil {
		return err
	}
	return deleteChunked(it, service)
}
