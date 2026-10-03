# Semiadmins: delegiertes Aktivieren und Deaktivieren

Datum: 2026-10-03. Diese Spec erweitert die Basis-Spec
`2026-09-20-jellyfin-enabler-design.md`; alles, was hier nicht geändert wird,
gilt dort unverändert weiter.

## Kontext & Ziel

Der Dienst (Go, Modul `jellyfinenabler`, nur Standardbibliothek) lässt heute
ausschließlich Jellyfin-Administratoren hinein. Nicht-Admins werden im Login
abgewiesen (`internal/jellyfin/client.go:118-126`, `ErrNotAdmin`; ausgewertet
nur in `internal/server/server.go:157`). Danach gibt es keine Rollenprüfung
mehr: Alle Jellyfin-Aufrufe laufen mit dem Token des eingeloggten Admins. Der
Dienst hat keine Persistenz und kein Volume; das Image ist distroless/nonroot.

Ziel: Auch nicht privilegierte Jellyfin-User („Semiadmins“) sollen bestimmte
andere User aktivieren und deaktivieren können, ohne Jellyfin-Adminrechte zu
bekommen. Typischer Fall: ein Elternteil schaltet die Konten der eigenen
Kinder.

Erfolgskriterien:

- Admins arbeiten unverändert weiter und pflegen zusätzlich die Zuordnung.
- Ein Semiadmin meldet sich mit seinem normalen Jellyfin-Konto an und sieht
  nur die ihm zugewiesenen User mit einem Aktiv/Aus-Schalter.
- Ein Semiadmin kann weder Bibliotheken ändern noch nicht zugewiesene User
  schalten.
- Die Zuordnung übersteht Neustarts.

## Entscheidungen

| Entscheidung | Wahl | Begründung |
|---|---|---|
| Zuordnung | Je Semiadmin eine eigene Liste schaltbarer User; keine globale Liste, keine Gruppen | „Kontrollierbar“ ist keine eigene Markierung am User, sondern ergibt sich aus der Zuordnung |
| Interner Admin-Zugang | Jellyfin-API-Key per ENV `JELLYFIN_API_KEY`, nicht Benutzername/Passwort | Kein Login, kein Token-Ablauf, kein Passwort im Container, einzeln widerrufbar. Nachteil: Jellyfins Aktivitätslog nennt keinen Benutzer, deshalb führt der Dienst ein eigenes Log |
| Speicher | Eine JSON-Datei, Pfad per ENV `SETTINGS_FILE` | Einfach, ohne Abhängigkeiten, passt zum Standardbibliothek-Ansatz |
| Admin-GUI | Abschnitt „Semiadmin“ auf der bestehenden Detailseite des jeweiligen Users | Keine neue Seite nötig |
| Architektur | Eigene, schmale Semiadmin-Pfade: eigene Ansicht, eigener Endpunkt, einziger Verwender des API-Keys | Siehe unten |

Verworfene Architekturen:

1. Gemeinsame Handler mit Token-Tausch und Filtern: Ein vergessener Filter
   bedeutet volle Adminrechte.
2. Alle Aufrufe über den API-Key: Das widerspricht „für Admins bleibt alles
   beim Alten“ und nimmt Jellyfin als zweite Prüfinstanz weg.

Admins nutzen weiter ihr eigenes Token. Der API-Key wird nur für
Semiadmin-Anfragen und die Startprüfung benutzt.

## Nicht-Ziele

- Keine Gruppen.
- Keine Zeitpläne oder Zeitlimits.
- Keine Bibliotheksänderungen durch Semiadmins.
- Kein Umbau des CSRF-Schutzes; er bleibt bei `SameSite=Lax` plus PUT mit
  JSON-Body.
- Kein automatisches Aufräumen verwaister Semiadmin-Einträge.

## Konfiguration und Deployment

Neue Umgebungsvariablen in `internal/config/config.go`, mit denselben
Trim-Regeln wie bei den bestehenden Variablen:

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `JELLYFIN_API_KEY` | leer | Leer bedeutet: Semiadmin-Funktion aus, Verhalten wie heute |
| `SETTINGS_FILE` | `/data/settings.json` | Pfad der Settings-Datei |

Der Key erscheint nie in der JSON-Datei, in Logs oder im HTML.

Deployment-Änderungen:

- `Dockerfile`: Das Verzeichnis `/data` wird in der Build-Stufe angelegt und
  mit `COPY --chown=65532:65532` in die distroless-Stufe übernommen, damit der
  nonroot-Prozess schreiben kann.
- `docker-compose.yml`: `JELLYFIN_API_KEY: "${JELLYFIN_API_KEY:-}"` (Wert aus
  `.env`), benanntes Volume `jellyfin-enabler-data:/data` samt Top-Level-Schlüssel
  `volumes:`.
- `.gitignore`: `.env` und `data/`.
- `Makefile`, Target `run`: setzt `SETTINGS_FILE ?= ./data/settings.json` für
  lokale Läufe.
- `README`: zwei neue Zeilen in der ENV-Tabelle, neuer Abschnitt
  „Semiadmins“, Hinweis, dass ein Bind-Mount statt des benannten Volumes dem
  Benutzer 65532 gehören muss, Ergänzung des Sicherheitshinweises (der API-Key
  ist ein vollwertiger Admin-Zugang).

Start in `cmd/server/main.go`:

- Ist der Key leer, erscheint die Log-Zeile „Semiadmin-Funktion aus
  (JELLYFIN_API_KEY nicht gesetzt)“.
- Ist er gesetzt, erfolgt ein einmaliger Aufruf `Users` mit dem Key. Schlägt er
  fehl, gibt es nur die Log-Warnung „JELLYFIN_API_KEY konnte nicht geprüft
  werden: …“ und keinen Abbruch, weil Jellyfin beim Start noch nicht
  erreichbar sein kann.

## Settings-Datei und Paket `internal/settings`

Format der Datei:

```json
{"version": 1, "semiadmins": {"<user-id-anna>": ["<user-id-ben>", "<user-id-clara>"]}}
```

Schlüssel und Listeneinträge sind Jellyfin-User-IDs, keine Namen, weil sich
Namen ändern können.

Schnittstelle des Pakets:

```go
func Load(path string) (*Store, error)
func (s *Store) Targets(semiadminID string) (targets []string, ok bool) // Kopie
func (s *Store) Semiadmins() map[string][]string                        // Kopie, für die Pillen in der Liste
func (s *Store) Set(semiadminID string, targets []string) error         // speichert sofort
func (s *Store) Remove(semiadminID string) error                        // speichert sofort
```

Verhalten:

- Der Zugriff läuft unter einem `sync.RWMutex`.
- Eine fehlende Datei gilt als leerer Stand.
- Unlesbares JSON oder eine unbekannte `version` bricht den Start mit klarer
  Fehlermeldung ab; es gibt keine stille Überschreibung.
- Schreiben ist atomar: `os.MkdirAll` auf das Verzeichnis (0700), Temp-Datei im
  selben Verzeichnis (0600), `fsync`, `os.Rename`.
- Erst nach erfolgreichem Schreiben wird der Stand im Speicher ersetzt.
  Schlägt das Schreiben fehl, bleibt der alte Stand gültig und der Handler
  antwortet mit 500 „Einstellungen konnten nicht gespeichert werden.“
- Die Datei wird auch bei leerem API-Key geladen. Ein vorhandener Stand bleibt
  so erhalten und wird wieder wirksam, sobald der Key gesetzt ist; ein Fehler
  in der Datei fällt sofort beim Start auf.

## Anmeldung, Sitzung und Rollen

`jellyfin.Session` (`types.go:31-35`) bekommt das Feld `Admin bool`.
`Authenticate` weist Nicht-Admins nicht mehr ab und meldet das Token nicht mehr
selbst ab; `ErrNotAdmin` entfällt im Client. `session.Session`
(`internal/session/store.go:16-21`) bekommt ebenfalls `Admin bool`, `Create`
wird entsprechend erweitert.

Entscheidung in `handleLogin` nach erfolgreichem `Authenticate`:

1. Jellyfin-Admin: Sitzung mit `Admin=true`.
2. Kein Admin, API-Key gesetzt und `settings.Targets(userID)` liefert `ok`:
   Sitzung mit `Admin=false` (Semiadmin).
3. Sonst: `Logout` mit dem gerade erhaltenen Token (Fehler nur loggen), Status
   403, Login-Seite mit „Dieses Konto hat keinen Zugriff auf diesen Dienst.“

Auf der Login-Seite wird die Feldbeschriftung „Administrator“ zu
„Benutzername“.

**Admin-Prüfung.** Ein neuer Wrapper `nurAdmin` umschließt `seite` und `api`.
Eine Sitzung ohne `Admin` wird auf Seiten nach `/` umgeleitet, die API
antwortet mit 403 JSON. Bisher erzwang Jellyfin die Adminrechte über das
Token; mit Semiadmin-Sitzungen muss der Dienst sie selbst erzwingen.

**Semiadmin-Prüfung.** Sie läuft bei jeder Semiadmin-Anfrage, nicht nur beim
Login, und gilt für genau zwei Routen: `GET /{$}`, wenn die Sitzung kein
`Admin` trägt, und `PUT /api/controlled/{id}/active`. Bedingungen:

- Der API-Key ist gesetzt.
- `settings.Targets(sess.UserID)` liefert `ok`.
- In der per API-Key geladenen Userliste existiert das eigene Konto und ist
  nicht deaktiviert.

Die Prüfung gilt nur dann als nicht bestanden, wenn der API-Key nicht gesetzt
ist, der Settings-Eintrag fehlt, oder die Userliste mit dem API-Key
erfolgreich geladen wurde und das eigene Konto darin fehlt oder deaktiviert
ist. In diesem Fall wird die Sitzung gelöscht und das User-Token per
Jellyfin-Logout entwertet (Fehler nur loggen); Seiten leiten auf `/login` um,
die API antwortet mit 401. Die Aktionen laufen über den API-Key. Ein
inzwischen gesperrter oder entmachteter Semiadmin könnte mit seiner alten
Sitzung sonst weiter schalten.

Scheitert der Key-Aufruf selbst, wird die Sitzung in keinem Fall gelöscht.
Bei 401 oder 403 von Jellyfin gilt die 502-Regel aus dem Abschnitt
„Fehlerbehandlung“. Bei allen anderen Fehlern (Netzwerk, Timeout, 5xx) greift
die bestehende Fehlerabbildung des Clients (`*jellyfin.Error` mit deutscher
Meldung) wie bei Aufrufen mit dem User-Token; Status und Meldung werden
durchgereicht.

**Admin-Prüfung bei `/delegation`.** Zusätzlich zu `nurAdmin` prüft der
Handler von `PUT /api/users/{id}/delegation`, dass in der mit dem Token des
Aufrufers geladenen Userliste das eigene Konto (`sess.UserID`) vorhanden und
Administrator ist. Sonst antwortet er mit 403 und löscht die Sitzung. Die
Settings-Datei ist eine Ressource des Dienstes, die Jellyfin nicht schützt;
ein in Jellyfin inzwischen entmachteter Admin mit alter Sitzung dürfte sonst
weiter Zuordnungen ändern. Die übrigen Admin-Routen brauchen das nicht, weil
dort Jellyfin das Token bei jedem schreibenden Aufruf selbst prüft.

Wird ein Semiadmin später Jellyfin-Admin, gilt ab dem nächsten Login die
Admin-Rolle.

## Jellyfin-Client

Neue Methode:

```go
func (c *Client) SetDisabled(ctx context.Context, token, userID string, disabled bool) (bool, error)
```

`SetDisabled` arbeitet als Read-Modify-Write auf der rohen Policy wie
`UpdateAccess` (`client.go:232 ff.`, `map[string]json.RawMessage`). Es ändert
ausschließlich `IsDisabled`, lässt `EnableAllFolders` und `EnabledFolders`
unberührt und bricht wie `UpdateAccess` ab, wenn `IsAdministrator` in der
gelesenen Policy fehlt. Ein gemeinsamer interner Helfer für das Lesen und
Schreiben der Policy sorgt dafür, dass `UpdateAccess` und `SetDisabled` nicht
auseinanderlaufen.

Der API-Key wird als `token`-Argument an die bestehenden Methoden (`Users`,
`SetDisabled`) übergeben und landet damit im vorhandenen Header
`Authorization: MediaBrowser Client="Jellyfin Enabler", …, Token="…"`.

Das Interface `Jellyfin` im Server (`server.go:23-30`) bekommt `SetDisabled`.
`server.New` bekommt zusätzlich den Settings-Store und den API-Key.

Der Semiadmin-Pfad ruft mit dem Key ausschließlich auf:

- `GET /Users`
- `GET /Users/{id}`
- `POST /Users/{id}/Policy` (mit nur geändertem `IsDisabled`)

## HTTP-Oberfläche

| Methode | Pfad | Rolle | Zweck |
|---|---|---|---|
| GET | `/{$}` | Admin, Semiadmin | Admin: Benutzerliste wie bisher, zusätzlich Pille „Semiadmin“ bei eingetragenen Usern. Semiadmin: neue Ansicht (Template `controlled.html`) |
| GET | `/users/{id}` | nur Admin | Detailseite |
| PUT | `/api/users/{id}/access` | nur Admin | Speichern von Aktiv-Status und Bibliotheken, sonst unverändert |
| PUT | `/api/users/{id}/delegation` | nur Admin | NEU: Semiadmin-Zuordnung speichern |
| PUT | `/api/controlled/{id}/active` | nur Semiadmin | NEU: zugewiesenen User schalten |

Alle übrigen Routen (`healthz`, `static`, `manifest`, `login`, `logout`)
bleiben unverändert.

### `PUT /api/users/{id}/delegation`

Nur für Admins. Body, maximal 64 KiB:

```json
{ "semiadmin": true, "users": ["<id>", "<id>"] }
```

Die Userliste wird mit dem Token des Admins geladen. Ablehnungen, in dieser
Reihenfolge:

- Das eigene Konto (`sess.UserID`) fehlt in der Userliste oder ist kein
  Administrator: 403, die Sitzung wird gelöscht (Begründung im Abschnitt
  „Anmeldung, Sitzung und Rollen“). Ebenso behandelt wird ein 403 von
  Jellyfin auf den Listenaufruf mit dem Token des Admins.
- API-Key nicht gesetzt: 409 „JELLYFIN_API_KEY ist nicht gesetzt.“
- `{id}` unbekannt: 404.
- `{id}` ist Administrator: 400.
- Ein Eintrag in `users` ist unbekannt, ist Administrator oder gleich `{id}`:
  400.

Duplikate werden entfernt. `semiadmin: false` führt zu `Remove`, `users` wird
ignoriert. `semiadmin: true` mit leerer Liste ist erlaubt: Der Semiadmin kann
sich anmelden und sieht eine leere Liste. Die Antwort enthält den gespeicherten
Stand im selben Format.

### `PUT /api/controlled/{id}/active`

Nur für Semiadmin-Sitzungen; eine Admin-Sitzung bekommt 403, Admins nutzen
`/access`. Body, maximal 64 KiB:

```json
{ "disabled": true }
```

Das Feld `disabled` ist Pflicht; fehlt es, antwortet der Dienst mit 400 und
schaltet nichts.

Prüfungen in dieser Reihenfolge:

1. Semiadmin-Prüfung aus dem Abschnitt „Anmeldung, Sitzung und Rollen“.
2. `{id}` steht in der eigenen Liste, sonst 403.
3. Das Ziel existiert in Jellyfin, sonst 404.
4. Das Ziel ist kein Administrator, sonst 403 „Administratoren können nicht
   geschaltet werden.“

Danach folgt `SetDisabled` mit dem API-Key. Die Antwort lautet
`{"disabled": bool}`. Je Schaltung entsteht eine Log-Zeile:

```
semiadmin schaltet: von=<Name> (<UserID>) ziel=<Name> (<ID>) disabled=<bool>
```

## Frontend

Alle UI-Texte sind deutsch, Tailwind wie bisher (`web/src/input.css` nach
`web/static/app.css`).

**`web/templates/user.html`.** Bei Nicht-Admins erscheint unter den
Bibliotheken ein neuer Abschnitt „Semiadmin“ mit der Checkbox „Darf andere
Benutzer aktivieren und deaktivieren“ und darunter „Darf schalten:“ mit Chips
aller Nicht-Admins außer dem User selbst (gleiches Chip-Muster wie bei den
Bibliotheken). Die Chips sind deaktiviert, solange die Checkbox aus ist. Bei
Administratoren fehlt der Abschnitt. Ohne API-Key zeigt der Abschnitt nur den
Hinweis „Semiadmin-Funktion ist aus: JELLYFIN_API_KEY ist nicht gesetzt.“

Es gibt einen „Speichern“-Knopf für die ganze Seite. `app.js` sendet nur die
geänderten Teile (`/access` und/oder `/delegation`), mit der vorhandenen
Wiederherstellung des alten Stands im Fehlerfall und mit Toasts.

Mockup der Detailseite:

```
‹ Zurück            Anna

[ Aktiv | Aus ]
☑ Alle Bibliotheken, auch künftig neue
(Filme) (Serien) (Musik)

── Semiadmin ──────────────────────
☑ Darf andere Benutzer aktivieren
  und deaktivieren

Darf schalten:
(Ben ✓) (Clara ✓) (Dora)

                      [ Speichern ]
```

**`web/templates/users.html`.** Die Karte zeigt zusätzlich die Pille
„Semiadmin“.

**`web/templates/controlled.html` (neu, für Semiadmins).** Der Header ist wie
bisher mit Abmelden, darunter steht je zugewiesenem User eine Karte mit Name
und Aktiv/Aus-Schalter. Es gibt keine Bibliotheken und keinen Link auf eine
Detailseite. Der Schalter speichert sofort per
`PUT /api/controlled/{id}/active`; bei einem Fehler springt er zurück und ein
Toast zeigt die Meldung. Bei leerer Liste erscheint der Text „Dir sind keine
Benutzer zugewiesen.“

## Fehlerbehandlung

- **API-Key ungültig oder widerrufen.** Antwortet Jellyfin auf einen
  Key-Aufruf mit 401 oder 403, gibt der Dienst Status 502 mit der Meldung
  „Interner Zugang nicht verfügbar. Bitte den Administrator informieren.“
  zurück und loggt den Fehler. Die Sitzung des Semiadmins bleibt bestehen.
  Die Semiadmin-Ansicht zeigt die Meldung im normalen Layout mit
  Abmelden-Knopf.
  Alle anderen Fehler des Key-Aufrufs (Netzwerk, Timeout, 5xx) werden über die
  bestehende Fehlerabbildung des Clients (`*jellyfin.Error` mit deutscher
  Meldung) durchgereicht wie bei Aufrufen mit dem User-Token; auch dann bleibt
  die Sitzung bestehen.
  Abgrenzung: Heute löscht ein 401 von Jellyfin die Sitzung
  (`server.go:264-271`, `:414-418`); das gilt weiter für Aufrufe mit dem
  User-Token, nicht für Aufrufe mit dem API-Key.
- **Verwaiste IDs (User in Jellyfin gelöscht).** Die Semiadmin-Ansicht zeigt
  nur IDs, die in Jellyfin als Nicht-Admin existieren. Die Detailseite bietet
  als Chips nur User an, die in Jellyfin als Nicht-Admin existieren; verwaiste
  IDs werden deshalb weder angezeigt noch mitgeschickt und fallen beim
  nächsten Speichern dieser Zuordnung weg.
  Das steht nicht im Widerspruch zur 400-Ablehnung unbekannter IDs, die nur
  manipulierte oder veraltete Eingaben trifft. Ein verwaister
  Semiadmin-Schlüssel bleibt wirkungslos in der Datei stehen.
- **Ziel wird nachträglich Administrator.** Das Ziel wird in der Semiadmin-Ansicht
  ausgefiltert und vom Endpunkt mit 403 abgelehnt.
- **Settings-Datei nicht schreibbar.** Der Dienst antwortet mit 500, der alte
  Stand bleibt gültig (siehe Abschnitt „Settings-Datei und Paket
  `internal/settings`“).

## Tests

- `internal/settings`: fehlende Datei, Rundlauf aus Laden und Speichern,
  kaputtes JSON, unbekannte Version, Schreibfehler lässt den Stand unverändert,
  parallele Zugriffe (`go test -race` läuft bereits in `make test`).
- `internal/config`: beide neuen ENV-Variablen samt Vorgaben.
- `internal/jellyfin`: `SetDisabled` ändert nur `IsDisabled` und reicht alle
  übrigen Policy-Felder unverändert durch (analog zu den Passthrough-Tests
  `client_test.go:365-504`); Abbruch bei fehlendem `IsAdministrator`;
  `Authenticate` liefert das Admin-Flag und meldet Nicht-Admins nicht mehr ab.
- `internal/server` (Muster: `fakeJF`, `aufbau`, `angemeldet` aus
  `server_test.go:20-93`):
  - Login-Entscheidung für Admin, Semiadmin, User ohne Rolle und Semiadmin bei
    fehlendem Key.
  - Rollenmatrix Admin, Semiadmin und ohne Sitzung gegen jede Route.
  - `/delegation` mit allen Ablehnungsgründen.
  - `/delegation` durch eine Admin-Sitzung, deren Konto in Jellyfin kein
    Administrator mehr ist, ergibt 403 und löscht die Sitzung.
  - `/controlled/{id}/active` mit fremder ID, Admin als Ziel und unbekanntem
    Ziel.
  - Rollenentzug und Deaktivierung des Semiadmins mitten in der Sitzung.
  - Key-Fehler: 401 oder 403 von Jellyfin ergibt 502, jeder andere Fehler wird
    durchgereicht; in beiden Fällen bleibt die Sitzung bestehen.
  - Eine Schaltung erzeugt die Log-Zeile.
  - Das HTML der Semiadmin-Ansicht enthält keine Bibliotheken und keine Links
    auf `/users/`.

## Verifikation vor der Umsetzung

Gegen die echte Jellyfin-Instanz (10.11) wird einmal geprüft, dass ein API-Key
im Header `Authorization: MediaBrowser … Token="<Key>"` angenommen wird:
`GET /Users` muss 200 liefern. Diese Annahme wurde am 03.10.2026 gegen die echte Instanz geprüft: `GET /Users` mit dem API-Key im Header lieferte 200.

## Risiken & Hinweise

- Der API-Key ist ein vollwertiger Admin-Zugang; er könnte über ENV oder
  Compose-Datei abfließen. Gegenmaßnahmen: Der Key steht nur in `.env`, wird nie
  geloggt, und sein Verwendungspfad ist schmal (siehe Abschnitt
  „Jellyfin-Client“).
- Verlust der Settings-Datei: Alle Semiadmins verlieren den Zugang, Admins sind
  nicht betroffen.
- Jellyfins Aktivitätslog nennt bei Semiadmin-Schaltungen keinen Benutzer;
  Ersatz ist die Log-Zeile des Dienstes.
- Der CSRF-Schutz bleibt wie in der Basis-Spec; die neuen Endpunkte sind wie
  `/access` PUT mit JSON-Body.
