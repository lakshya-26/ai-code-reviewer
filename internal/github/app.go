package github

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v62/github"
)

// NewInstallationClient creates an authenticated GitHub client scoped to one installation.
//
// privateKeySource is either:
//   - A file system path to a .pem file  (e.g. "/etc/secrets/key.pem")
//   - Raw PEM content starting with "-----BEGIN" (for Railway/Render env vars)
//
// The ghinstallation transport handles JWT creation and automatic token refresh
// transparently — callers never touch raw tokens.
func NewInstallationClient(appID, installationID int64, privateKeySource string) (*github.Client, error) {
	privateKey, err := resolvePrivateKey(privateKeySource)
	if err != nil {
		return nil, err
	}

	itr, err := ghinstallation.New(
		http.DefaultTransport,
		appID,
		installationID,
		privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("creating installation transport for installation %d: %w", installationID, err)
	}

	return github.NewClient(&http.Client{Transport: itr}), nil
}

// resolvePrivateKey returns raw PEM bytes from either a file path or inline PEM content.
func resolvePrivateKey(source string) ([]byte, error) {
	source = strings.TrimSpace(source)

	if strings.HasPrefix(source, "-----BEGIN") {
		return []byte(source), nil
	}

	key, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("reading private key file %q: %w", source, err)
	}
	return key, nil
}
