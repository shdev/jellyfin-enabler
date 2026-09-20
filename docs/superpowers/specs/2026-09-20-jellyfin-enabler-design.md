# Jellyfin Enabler: Benutzer aktivieren/deaktivieren und Bibliotheken zuordnen

Datum: 2026-09-20
Status: freigegeben

## Kontext & Ziel

Auf dem Heimserver läuft Jellyfin 10.11.0. Für den Alltag fehlt ein kleiner
Handgriff: einem Benutzer schnell den Zugang abdrehen oder ihm einzelne
Bibliotheken wegnehmen, ohne sich durch das Jellyfin-Dashboard zu klicken.

Dieser Dienst ist ein Go-Webservice mit genau diesem einen Zweck. Er meldet
sich mit einem Jellyfin-Administrator an und schreibt über die Jellyfin-API
zwei Dinge pro Benutzer: den Aktiv-Status und die Bibliotheksfreigaben.
Bedient wird er im Browser, auch vom Handy.

Das Referenzprojekt `yt-dl-web-client` (Nachbarverzeichnis) liefert
Design-System, Makefile, Docker-Aufbau und Codekonventionen; dieser Dienst
folgt ihnen bewusst, damit beide Projekte gleich anzufassen sind.

## Verifizierte Grundlagen (Jellyfin 10.11.0)

Alle Angaben wurden gegen die OpenAPI-Spezifikation und gegen den Quellcode
am Tag `v10.11.0` geprüft, nicht aus dem Gedächtnis übernommen.

| Zweck | Endpunkt | Berechtigung |
|---|---|---|
| Anmelden | `POST /Users/AuthenticateByName` | keine |
| Abmelden | `POST /Sessions/Logout` | angemeldet |
| Benutzer auflisten | `GET /Users` | angemeldet |
| Einzelner Benutzer | `GET /Users/{userId}` | angemeldet |
| Policy schreiben | `POST /Users/{userId}/Policy` | `RequiresElevation` |
| Bibliotheken auflisten | `GET /Library/VirtualFolders` | `FirstTimeSetupOrElevated` |

Relevante Felder:

- `UserDto`: `Id`, `Name`, `Policy`
- `UserPolicy`: `IsDisabled`, `EnabledFolders` (Liste von Bibliotheks-IDs),
  `EnableAllFolders`, `IsAdministrator` — insgesamt 44 Felder
- `VirtualFolderInfo`: `Name`, `ItemId`, `CollectionType`.
  `ItemId` ist der Wert, der in `EnabledFolders` steht.

Serverseitige Regeln aus `Jellyfin.Api/Controllers/UserController.cs`
(`UpdateUserPolicy`), die das Verhalten des Dienstes bestimmen:

1. Administratoren lassen sich nicht deaktivieren → `403`.
2. Der letzte aktive Benutzer lässt sich nicht deaktivieren → `403`.
3. Dem letzten Administrator lässt sich die Administratorrolle nicht
   entziehen → `403`. Dieser Dienst kann den Fall nicht auslösen, weil er
   `IsAdministrator` unverändert durchreicht; die Meldung wird trotzdem
   abgebildet.
4. Beim Deaktivieren ruft Jellyfin selbst `RevokeUserTokens` auf: laufende
   Sitzungen des Benutzers fliegen raus. Der Dienst muss sich darum nicht
   kümmern.

Authentifizierungs-Header (`Jellyfin.Server.Implementations/Security/AuthorizationContext.cs`):
`Authorization: MediaBrowser Token="…", Client="…", Device="…", DeviceId="…", Version="…"`.
Das ältere `X-Emby-Authorization` greift in 10.11 nur noch, wenn
`EnableLegacyAuthorization` gesetzt ist, und wird deshalb nicht verwendet.
`X-Emby-Token`/`X-MediaBrowser-Token` funktionieren weiterhin, sind aber der
alte Weg.

## Entscheidungen

1. **Web-UI, keine CLI.** Bedienung im Browser, auch vom Handy.
2. **Anmeldung mit dem Jellyfin-Admin-Konto**, nicht mit einem statischen
   API-Key in der Konfiguration. Kein zusätzliches Geheimnis, und der Schutz
   der Oberfläche ist derselbe wie der von Jellyfin. Nicht-Administratoren
   werden nach erfolgreicher Anmeldung abgewiesen.
3. **Eine Detailseite pro Benutzer.** Änderungen werden auf der Seite
   gesammelt und mit einem Speichern-Knopf in *einem* Request abgeschickt —
   nicht bei jedem Klick. Das hält die Anzahl der Requests klein und das
   Zeitfenster für Kollisionen minimal.
4. **Die Policy wird als `map[string]json.RawMessage` durchgereicht.**
   Siehe „Policy-Passthrough" — die zentrale Entscheidung dieses Designs.
5. **`EnableAllFolders` wird in der Oberfläche sichtbar gemacht**, nicht
   hinter „alle Häkchen gesetzt" versteckt.
6. **Keine externen Go-Abhängigkeiten.** `net/http`-ServeMux mit
   Methoden-Patterns, `html/template`, `embed.FS` — wie im Referenzprojekt.
7. **Design-System aus `yt-dl-web-client`** übernehmen: Tokens, `.card`,
   `.btn`, `.pill`, `.seg`, `.chip`, `.toast`; Tailwind v4 über Docker
   kompiliert, kein lokales npm.
8. **Docker neben Jellyfin**, Host-Port 8081 (weicht dem Referenzprojekt auf
   8080 aus, falls beide auf derselben Maschine laufen).

## Nicht-Ziele

- Benutzer anlegen oder löschen
- Passwörter ändern oder zurücksetzen
- Laufende Sitzungen anzeigen oder beenden
- Andere Policy-Felder als die drei genannten bearbeiten
- Mehrere Jellyfin-Server
- Mehrbenutzerbetrieb des Dienstes selbst (mehrere gleichzeitige Admins
  sind technisch möglich, aber kein Entwurfsziel)

## Architektur

### Repo-Layout

```
cmd/server/main.go        Konfiguration, Server, sauberes Herunterfahren, -healthcheck
cmd/genicons/main.go      PWA-Icons erzeugen (portiert, andere Glyphe)
internal/config/          Laufzeitkonfiguration aus Umgebungsvariablen
internal/jellyfin/        Jellyfin-API-Client — kennt kein HTTP-Frontend
internal/session/         Cookie -> {Token, UserID, Name}, im Speicher, mit TTL
internal/server/          HTTP-Handler und Templates
web/embed.go              //go:embed templates static
web/templates/            layout.html, login.html, users.html, user.html
web/src/input.css         Tailwind-Quelle mit Design-Tokens
web/static/               app.css (generiert), app.js, icons/, manifest.webmanifest
Makefile · Dockerfile · docker-compose.yml · .dockerignore · README.md
```

Abhängigkeitsrichtung: `server` kennt `jellyfin` und `session`, umgekehrt nie.
Der Client ist damit ohne HTTP-Server testbar, die Handler ohne echtes
Jellyfin.

Modulname: `jellyfinenabler`.

### Policy-Passthrough

`POST /Users/{userId}/Policy` **ersetzt die Policy vollständig**; ein PATCH
gibt es nicht. Würde der Dienst die Policy in ein Go-Struct mit nur den
benötigten Feldern lesen und zurückschreiben, löschte er die übrigen 41
Felder — Transcoding-Rechte, Zugriffszeitfenster, `MaxParentalRating`,
Sitzungslimits. Das ist der Fehler, in den selbstgebaute Jellyfin-Werkzeuge
regelmäßig laufen.

Deshalb:

```go
type user struct {
    Id     string                     `json:"Id"`
    Name   string                     `json:"Name"`
    Policy map[string]json.RawMessage `json:"Policy"`
}
```

Beim Schreiben werden ausschließlich diese drei Schlüssel ersetzt:

- `IsDisabled`
- `EnableAllFolders`
- `EnabledFolders`

Alles andere geht byteidentisch zurück, auch Felder, die erst ein Plugin oder
eine spätere Jellyfin-Version einführt. Fürs Anzeigen wird zusätzlich ein
kleines typisiertes Struct dekodiert — ausschließlich lesend.

Fehlt `Policy` in der Antwort, bricht der Vorgang mit einem Fehler ab; es
wird keine Policy neu erfunden.

Ablauf eines Speichervorgangs:

1. `GET /Users/{id}` — Policy frisch lesen
2. die drei Schlüssel setzen
3. `POST /Users/{id}/Policy` mit der veränderten Map
4. den geschriebenen Zustand an die Oberfläche zurückgeben

### `EnableAllFolders`-Semantik

Ist `EnableAllFolders = true`, ignoriert Jellyfin `EnabledFolders`, und der
Benutzer sieht **auch jede künftig angelegte Bibliothek**. „Alle Häkchen
gesetzt" ist also nicht dasselbe wie „alle Bibliotheken erlaubt".

Die Oberfläche bildet beide Ebenen ab: ein Schalter „Alle Bibliotheken, auch
künftig neue" und darunter die Einzelauswahl, die ausgegraut ist, solange der
Schalter an ist. Die Einzelauswahl bleibt dabei erhalten und wird
mitgespeichert — wird der Schalter später ausgeschaltet, steht die frühere
Auswahl wieder da. Genauso verhält sich das Jellyfin-Dashboard.

### Authentifizierung und Sitzung

Anmelden:

1. `POST /Users/AuthenticateByName` mit `{"Username": …, "Pw": …}` und dem
   `Authorization`-Header ohne `Token`.
2. Die Antwort (`AuthenticationResult`) liefert `AccessToken` und
   `User.Policy.IsAdministrator`.
3. Ist der Benutzer kein Administrator, wird die Anmeldung abgelehnt **und
   das gerade erzeugte Token sofort per `POST /Sessions/Logout` wieder
   entwertet**, damit keine verwaiste Jellyfin-Sitzung zurückbleibt.
4. Sonst: Sitzung anlegen, Cookie setzen.

`DeviceId` ist stabil pro Benutzername: `sha256("jellyfin-enabler|" +
username)`, hex, auf 32 Zeichen gekürzt. Ohne das legt jeder Neustart des
Containers einen neuen Geräteeintrag in Jellyfin an.

`Client` = `Jellyfin Enabler`, `Device` = `Jellyfin Enabler`, `Version` =
Build-Version (`-ldflags`, Vorgabe `dev`).

Sitzungsspeicher: `map[string]session` im Speicher, geschützt durch ein
`sync.RWMutex`, mit absoluter TTL (Vorgabe 12 h) und einem Aufräum-Ticker.
Ein Neustart des Dienstes bedeutet: neu anmelden. Das ist gewollt — kein
persistenter Token auf der Platte.

Cookie: Name `jfe_session`, Wert 32 zufällige Bytes (`crypto/rand`,
base64url), `HttpOnly`, `SameSite=Lax`, `Path=/`. `Secure` wird bei `COOKIE_SECURE=true` immer gesetzt, bei `auto` (Vorgabe)
nur, wenn die Anfrage direkt über TLS kam. Hinter einem TLS-terminierenden
Reverse Proxy ist `COOKIE_SECURE=true` nötig, weil der Dienst dort im Klartext
angesprochen wird.

Abmelden entwertet das Jellyfin-Token per `POST /Sessions/Logout` und löscht
Cookie und Sitzungseintrag.

### HTTP-Oberfläche

| Route | Zweck |
|---|---|
| `GET /login` | Anmeldeformular; leitet bei gültiger Sitzung auf `/` |
| `POST /login` | Formular-POST mit `username`/`password` → Cookie |
| `POST /logout` | Jellyfin-Logout, Cookie löschen |
| `GET /{$}` | Benutzerliste, servergerendert |
| `GET /users/{id}` | Detailseite, Ausgangszustand servergerendert |
| `PUT /api/users/{id}/access` | Speichern, ein Read-Modify-Write |
| `GET /static/…` | eingebettete Assets |
| `GET /manifest.webmanifest` | PWA-Manifest |
| `GET /healthz` | Docker-Healthcheck, ohne Anmeldung |

Alle Routen außer `/login`, `/healthz` und `/static/…`
erfordern eine gültige Sitzung. Fehlt sie, antworten Seitenrouten mit einer
Weiterleitung auf `/login`, API-Routen mit `401`.

An- und Abmeldung laufen als klassische Formulare, damit sie ohne
JavaScript funktionieren; JSON bleibt allein dem Speichern vorbehalten.

Die Benutzerliste zeigt pro Benutzer: Name, Aktiv-Pille, die
Bibliotheksfreigabe als „Alle Bibliotheken" oder „5 von 7", und bei
Administratoren eine Markierung.

`PUT /api/users/{id}/access`:

```json
{ "disabled": false, "allFolders": false, "folders": ["a1b2…", "c3d4…"] }
```

Antwort `200` mit demselben Format — dem *bestätigten* Zustand nach dem
Schreiben. Die Oberfläche übernimmt diese Antwort, statt ihren eigenen
optimistischen Zustand stehen zu lassen.

Validierung vor dem Schreiben: jede ID in `folders` muss zu einer bekannten
Bibliothek aus `GET /Library/VirtualFolders` gehören, sonst `400`. So landet
kein Unsinn in der Policy.

Global gilt `Cache-Control: no-store` (Middleware `noStore` wie im
Referenzprojekt).

### Frontend

Servergerendertes HTML plus eine kleine `app.js` — dieselbe Aufteilung wie im
Referenzprojekt, kein HTMX, kein Build-Schritt für JavaScript.

`app.js` leistet genau drei Dinge:

1. Ausgangszustand und Arbeitszustand getrennt halten, Abweichungen zählen
2. den Speichern-Knopf beschriften (`Speichern (2)`) und bei null Abweichungen
   deaktivieren
3. den `PUT` schicken, die Antwort übernehmen, einen Toast zeigen

Beide Seiten bleiben ohne JavaScript lesbar; ohne JavaScript lässt sich nur
nicht speichern.

Aus `web/src/input.css` des Referenzprojekts werden Tokens und Komponenten
übernommen; die auftragsspezifischen Klassen `.bar`, `.bar-fill` und
`.job-card` entfallen. Bibliotheken werden als `.chip` dargestellt
(Checkbox + Label, angehakt = `accent-soft`), der Aktiv-Status als `.pill`.

PWA: Manifest, Apple-Touch-Icon, Safe-Area-Padding und Theme-Color wie im
Referenzprojekt. `cmd/genicons` wird portiert; die Glyphe wird ein
Kippschalter statt des Download-Pfeils.

### Build und Deployment

Makefile-Ziele analog zum Referenzprojekt: `build`, `test`,
`check` (= `fmt-check` + `vet` + `test`), `fmt-check`, `vet`, `run`, `image`,
`image-native`, `push`, `check-ghcr-user`, `up`, `down`, `start`, `stop`,
`css`, `css-watch`, `clean`.

Abweichungen: `IMAGE := jellyfin-enabler`, `HOST_PORT ?= 8081`, eigenes
npm-Cache-Volume `jellyfin-enabler-npm-cache`, und `run` setzt `PORT` sowie
`JELLYFIN_URL` statt der Download-Pfade. `TAILWIND_VERSION` bleibt bei
`4.3.3` und muss mit dem `ARG` im Dockerfile übereinstimmen.

Dockerfile, drei Stufen:

1. `node:22-alpine` — Tailwind kompilieren, damit nie ein veralteter
   committeter `app.css` ins Binary gerät
2. `golang:1.24` — `CGO_ENABLED=0 go build`
3. `gcr.io/distroless/static-debian12:nonroot` — bringt CA-Zertifikate mit
   (nötig, falls Jellyfin über HTTPS erreicht wird) und läuft als nonroot

`HEALTHCHECK` ruft `app -healthcheck` auf, wie im Referenzprojekt: das Binary
setzt mit dem Flag einen `GET` auf `http://127.0.0.1:$PORT/healthz` ab und
beendet sich mit 0 oder 1, statt den Server zu starten.

`docker-compose.yml` veröffentlicht `8081:8080` und setzt `JELLYFIN_URL`.

Konfiguration über Umgebungsvariablen (`internal/config`):

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `JELLYFIN_URL` | — (Pflicht) | Basis-URL, z. B. `http://jellyfin:8096` |
| `PORT` | `8080` | Port im Container |
| `SESSION_TTL` | `12h` | Lebensdauer einer Anmeldung |
| `COOKIE_SECURE` | `auto` | `auto` = nur bei direktem TLS; hinter TLS-Proxy auf `true` setzen |
| `HTTP_TIMEOUT` | `15s` | Zeitlimit für Jellyfin-Aufrufe |

Fehlt `JELLYFIN_URL`, startet der Dienst nicht, sondern beendet sich mit
einer verständlichen Meldung.

## Fehlerbehandlung

Jellyfin antwortet auf die drei Verbotsfälle mit `403` und einem englischen
Klartext im Rumpf. Der Statuscode allein ist mehrdeutig, deshalb wird der
Rumpf ausgewertet und auf deutsche Meldungen abgebildet:

| Rumpf enthält | Meldung |
|---|---|
| `Administrators cannot be disabled` | Administratoren können nicht deaktiviert werden. |
| `at least one enabled user` | Es muss mindestens ein aktiver Benutzer übrig bleiben. |
| `administrative access` | Es muss mindestens ein Administrator übrig bleiben. (durch den Passthrough nicht auslösbar, defensiv) |
| sonst | Jellyfin hat die Änderung abgelehnt. (Originaltext wird geloggt) |

Weitere Fälle:

- `401` von Jellyfin → Token abgelaufen: Sitzung verwerfen, Seitenrouten
  leiten auf `/login`, API-Routen antworten `401`
- `404` beim Benutzer → `404` mit Hinweis, dass der Benutzer nicht mehr
  existiert
- Netzwerk- oder Zeitüberschreitungsfehler → `502` mit Toast
- unbekannte Bibliotheks-ID im Request → `400`
- ungültige Anmeldedaten → `401` mit „Anmeldung fehlgeschlagen."; Benutzer-
  und Passwortfehler werden nicht unterschieden

Der Aktiv-Schalter ist bei Administratoren von vornherein ausgegraut, damit
Fall 1 in der Oberfläche gar nicht erst provoziert wird. Die serverseitige
Behandlung bleibt trotzdem bestehen — die Oberfläche ist Bequemlichkeit,
nicht die Durchsetzung.

Fehler aus dem `PUT` lassen die Oberfläche auf den Serverzustand
zurückspringen, statt eine Änderung vorzutäuschen.

## Verifikation

Testgetrieben: zu jedem Verhalten zuerst der Test.

`internal/jellyfin` gegen einen `httptest.Server`, der die sechs Endpunkte
fälscht:

- **`TestUpdateAccessPreservesUnknownPolicyFields`** — der wichtigste Test:
  eine Policy mit allen 44 Feldern plus einem erfundenen Zusatzfeld geht
  hinein, `IsDisabled` wird geändert, und alle übrigen Felder müssen
  byteidentisch wieder herauskommen
- `Authorization`-Header wird korrekt zusammengesetzt (mit und ohne Token)
- Anmeldung eines Nicht-Administrators wird abgelehnt und das Token
  entwertet
- die drei `403`-Rümpfe werden auf die deutschen Meldungen abgebildet
- `401` wird als abgelaufene Sitzung erkannt
- `EnableAllFolders` und `EnabledFolders` werden gemeinsam geschrieben

`internal/session`: Anlegen, Nachschlagen, Ablauf durch TTL, Löschen,
unbekanntes Cookie.

`internal/config`: Vorgaben und Überschreibungen als Tabellentest,
fehlendes `JELLYFIN_URL` als Fehler.

`internal/server` über `httptest` mit einem gefälschten Client:

- ohne Sitzung: Seitenrouten leiten weiter, API-Routen geben `401`
- Anmeldung setzt Cookie, Abmeldung löscht es
- Liste rendert Benutzer mit Status und Bibliothekszählung
- Detailseite rendert Chips, bei Administratoren ausgegrauten Schalter
- `PUT` schreibt und gibt den bestätigten Zustand zurück
- `PUT` mit unbekannter Bibliotheks-ID → `400`
- `403` von Jellyfin erreicht die Oberfläche als deutsche Meldung
- `Cache-Control: no-store` auf allen Antworten

`web`: Embed-Test, dass Templates und Assets im Binary liegen.

Abschließende Prüfungen von Hand:

- `make check` — gofmt, go vet, alle Tests
- `make css && make image-native && make start` — Container startet, UI
  erreichbar
- **Rauchtest gegen das echte Jellyfin 10.11.0**: anmelden, einen Testbenutzer
  deaktivieren und wieder aktivieren, eine Bibliothek entziehen und
  zurückgeben, danach im Jellyfin-Dashboard prüfen, dass die übrigen
  Policy-Felder dieses Benutzers unverändert sind. Dieser Schritt lässt sich
  nicht automatisieren und ist Voraussetzung dafür, das Projekt als fertig zu
  bezeichnen.

## Risiken & Hinweise

- **Der Rauchtest braucht eine erreichbare Jellyfin-Instanz und ein
  Admin-Konto.** Die automatisierten Tests laufen vollständig gegen Fakes; ob
  der Dienst mit dem echten Server spricht, zeigt erst dieser Schritt.
- **Die Fehlermeldungszuordnung hängt an englischen Textbausteinen** aus dem
  Jellyfin-Quellcode. Ändert Jellyfin die Formulierung, greift die
  Rückfallmeldung — der Dienst bleibt funktionsfähig, die Meldung wird nur
  unspezifisch. Der Originaltext wird geloggt.
- **Kollisionen bei paralleler Bearbeitung** sind nicht ausgeschlossen: wer
  gleichzeitig im Jellyfin-Dashboard dieselbe Policy ändert, dessen Änderung
  kann überschrieben werden. Das Zeitfenster zwischen Lesen und Schreiben
  liegt im Millisekundenbereich, und betroffen sind nur die drei verwalteten
  Felder. Optimistisches Sperren wäre möglich, ist für den Heimgebrauch aber
  unverhältnismäßig.
- **Der Sitzungsspeicher liegt im Arbeitsspeicher.** Neustart bedeutet neu
  anmelden.
- **Der Dienst gehört nicht ungeschützt ins Internet.** Wer die Oberfläche
  erreicht und ein Admin-Passwort hat, kann Zugänge abschalten. Er ist für
  das Heimnetz gedacht, hinter Reverse Proxy und TLS.
- **Jellyfin 10.11.0 ist nicht die neueste Version.** Die veröffentlichte
  OpenAPI-Spezifikation beschreibt inzwischen 12.1.0. Dieses Design richtet
  sich nach dem Quellcode von 10.11.0; bei einem Upgrade ist zu prüfen, ob
  `POST /Users/{userId}/Policy` und `GET /Library/VirtualFolders` unverändert
  sind.
