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
	"net/url"
	"sort"
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

// rawUser hält die Policy bewusst als Rohdaten. POST /Users/{id}/Policy
// ersetzt die Policy vollständig — würde hier ein Struct mit nur den
// benötigten Feldern stehen, gingen beim Zurückschreiben alle übrigen
// verloren (Spec §Policy-Passthrough).
type rawUser struct {
	ID     string                     `json:"Id"`
	Name   string                     `json:"Name"`
	Policy map[string]json.RawMessage `json:"Policy"`
}

// view dekodiert aus der Rohpolicy die Felder, die die Oberfläche anzeigt —
// ausschließlich lesend.
func (r rawUser) view() (User, error) {
	if r.Policy == nil {
		return User{}, fmt.Errorf("jellyfin: Benutzer %q (%s) hat keine Policy", r.Name, r.ID)
	}
	var p struct {
		IsAdministrator  bool     `json:"IsAdministrator"`
		IsDisabled       bool     `json:"IsDisabled"`
		EnableAllFolders bool     `json:"EnableAllFolders"`
		EnabledFolders   []string `json:"EnabledFolders"`
	}
	b, err := json.Marshal(r.Policy)
	if err != nil {
		return User{}, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return User{}, err
	}
	return User{
		ID:    r.ID,
		Name:  r.Name,
		Admin: p.IsAdministrator,
		Access: Access{
			Disabled:   p.IsDisabled,
			AllFolders: p.EnableAllFolders,
			Folders:    p.EnabledFolders,
		},
	}, nil
}

func (c *Client) Users(ctx context.Context, token string) ([]User, error) {
	var raw []rawUser
	if err := c.do(ctx, http.MethodGet, "/Users", token, "", nil, &raw); err != nil {
		return nil, err
	}
	users := make([]User, 0, len(raw))
	for _, r := range raw {
		u, err := r.view()
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name)
	})
	return users, nil
}

func (c *Client) User(ctx context.Context, token, userID string) (User, error) {
	r, err := c.rawUser(ctx, token, userID)
	if err != nil {
		return User{}, err
	}
	return r.view()
}

func (c *Client) rawUser(ctx context.Context, token, userID string) (rawUser, error) {
	var r rawUser
	if err := c.do(ctx, http.MethodGet, "/Users/"+url.PathEscape(userID), token, "", nil, &r); err != nil {
		return rawUser{}, err
	}
	return r, nil
}

func (c *Client) Libraries(ctx context.Context, token string) ([]Library, error) {
	var raw []struct {
		Name   string `json:"Name"`
		ItemID string `json:"ItemId"`
	}
	if err := c.do(ctx, http.MethodGet, "/Library/VirtualFolders", token, "", nil, &raw); err != nil {
		return nil, err
	}
	libs := make([]Library, 0, len(raw))
	for _, r := range raw {
		libs = append(libs, Library{ID: r.ItemID, Name: r.Name})
	}
	sort.Slice(libs, func(i, j int) bool {
		return strings.ToLower(libs[i].Name) < strings.ToLower(libs[j].Name)
	})
	return libs, nil
}

// UpdateAccess liest die Policy frisch, ersetzt genau drei Schlüssel und
// schreibt sie zurück. POST /Users/{id}/Policy ersetzt die Policy vollständig
// — deshalb Read-Modify-Write über die Rohdaten, damit die übrigen 41 Felder
// unangetastet bleiben (Spec §Policy-Passthrough).
func (c *Client) UpdateAccess(ctx context.Context, token, userID string, a Access) (Access, error) {
	r, err := c.rawUser(ctx, token, userID)
	if err != nil {
		return Access{}, err
	}
	if r.Policy == nil {
		return Access{}, fmt.Errorf("jellyfin: Benutzer %s hat keine Policy", userID)
	}
	// Jellyfin erwartet eine Liste, kein null.
	folders := a.Folders
	if folders == nil {
		folders = []string{}
	}
	set := func(name string, wert any) error {
		b, err := json.Marshal(wert)
		if err != nil {
			return err
		}
		r.Policy[name] = b
		return nil
	}
	if err := set("IsDisabled", a.Disabled); err != nil {
		return Access{}, err
	}
	if err := set("EnableAllFolders", a.AllFolders); err != nil {
		return Access{}, err
	}
	if err := set("EnabledFolders", folders); err != nil {
		return Access{}, err
	}
	path := "/Users/" + url.PathEscape(userID) + "/Policy"
	if err := c.do(ctx, http.MethodPost, path, token, "", r.Policy, nil); err != nil {
		return Access{}, err
	}
	return Access{Disabled: a.Disabled, AllFolders: a.AllFolders, Folders: folders}, nil
}
