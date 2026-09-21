# Jellyfin Enabler

Kleine Weboberfläche, um Jellyfin-Benutzer zu aktivieren bzw. zu deaktivieren
und ihnen Bibliotheken zuzuordnen. Angemeldet wird sich mit einem
Jellyfin-Administrator; der Dienst speichert selbst keine Zugangsdaten.

Geprüft gegen **Jellyfin 10.11.0**.

## Betrieb

**Vor dem ersten Start** `JELLYFIN_URL` in `docker-compose.yml` auf die eigene
Instanz setzen. Der mitgelieferte Wert `http://jellyfin:8096` ist nur
auflösbar, wenn Jellyfin im selben Docker-Netz läuft — sonst startet der
Dienst zwar und meldet sich als gesund, aber jede Anmeldung schlägt fehl.
Läuft Jellyfin in einem eigenen Compose-Projekt, zusätzlich dessen Netz
eintragen; beide Stellen sind in `docker-compose.yml` kommentiert.

```bash
docker compose up -d --build
```

Danach erreichbar unter <http://localhost:8081>.

## Konfiguration

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `JELLYFIN_URL` | — (Pflicht) | Basis-URL, z. B. `http://jellyfin:8096` |
| `PORT` | `8080` | Port **im Container**. Wer ihn ändert, muss auch das Port-Mapping in `docker-compose.yml` anpassen — sonst ist der Dienst vom Host aus nicht mehr erreichbar, obwohl der Container gesund bleibt. |
| `SESSION_TTL` | `12h` | Lebensdauer einer Anmeldung |
| `COOKIE_SECURE` | `auto` | hinter TLS-Proxy auf `true` setzen |
| `HTTP_TIMEOUT` | `15s` | Zeitlimit für Jellyfin-Aufrufe |

## Entwicklung

```bash
make check      # gofmt, go vet, Tests
make css        # Tailwind im Container übersetzen
make run        # lokal starten, JELLYFIN_URL=… überschreibbar
make start      # Container bauen, starten und die UI öffnen
```

Design, Makefile und Docker-Aufbau folgen dem Schwesterprojekt
`yt-dl-web-client`.

## Was der Dienst bewusst nicht kann

Benutzer anlegen oder löschen, Passwörter ändern, Sitzungen beenden. Dafür
gibt es das Jellyfin-Dashboard.

## Sicherheitshinweis

Wer die Oberfläche erreicht und ein Admin-Passwort hat, kann Zugänge
abschalten. Der Dienst gehört ins Heimnetz, hinter Reverse Proxy und TLS —
nicht ungeschützt ins Internet.
