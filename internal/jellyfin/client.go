package jellyfin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// clientName taucht in Jellyfin als App- und Gerätename der Sitzung auf.
const clientName = "Jellyfin Enabler"

// ErrNotAdmin meldet eine gültige Anmeldung ohne Administratorrechte. Der
// Dienst schreibt Policies und braucht RequiresElevation.
var ErrNotAdmin = errors.New("dieser Benutzer ist kein Administrator")

type Client struct {
	base    string
	http    *http.Client
	version string
}

func New(baseURL string, timeout time.Duration, version string) *Client {
	return &Client{
		base:    strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		version: version,
	}
}

// DeviceID ist pro Benutzername stabil. Ohne das legte jeder Neustart des
// Containers einen neuen Geräteeintrag in Jellyfin an
// (Spec §Authentifizierung und Sitzung).
func DeviceID(username string) string {
	sum := sha256.Sum256([]byte("jellyfin-enabler|" + username))
	return hex.EncodeToString(sum[:])[:32]
}

// authHeader baut den Authorization-Header im MediaBrowser-Format, das
// Jellyfin 10.11 erwartet. DeviceId wird nur bei der Anmeldung gebraucht;
// danach identifiziert das Token die Sitzung.
func (c *Client) authHeader(token, deviceID string) string {
	parts := []string{
		fmt.Sprintf("Client=%q", clientName),
		fmt.Sprintf("Device=%q", clientName),
		fmt.Sprintf("Version=%q", c.version),
	}
	if deviceID != "" {
		parts = append(parts, fmt.Sprintf("DeviceId=%q", deviceID))
	}
	if token != "" {
		parts = append(parts, fmt.Sprintf("Token=%q", token))
	}
	return "MediaBrowser " + strings.Join(parts, ", ")
}

// do führt einen Aufruf aus. body und out dürfen nil sein.
func (c *Client) do(ctx context.Context, method, path, token, deviceID string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader(token, deviceID))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// Begrenzt lesen: der Rumpf ist ein kurzer Klartext, kein Datenstrom.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statusError(resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Authenticate(ctx context.Context, username, password string) (Session, error) {
	var res struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Policy struct {
				IsAdministrator bool `json:"IsAdministrator"`
			} `json:"Policy"`
		} `json:"User"`
	}
	body := map[string]string{"Username": username, "Pw": password}
	err := c.do(ctx, http.MethodPost, "/Users/AuthenticateByName", "", DeviceID(username), body, &res)
	if err != nil {
		return Session{}, err
	}
	if !res.User.Policy.IsAdministrator {
		// Kein verwaistes Token zurücklassen; Fehler dabei sind nachrangig.
		_ = c.Logout(ctx, res.AccessToken)
		return Session{}, ErrNotAdmin
	}
	return Session{Token: res.AccessToken, UserID: res.User.ID, Name: res.User.Name}, nil
}

func (c *Client) Logout(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, "/Sessions/Logout", token, "", nil, nil)
}
