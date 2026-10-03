# Jellyfin Enabler

Kleine Weboberfläche, um Jellyfin-Benutzer zu aktivieren bzw. zu deaktivieren
und ihnen Bibliotheken zuzuordnen. Angemeldet wird sich mit einem
Jellyfin-Administrator oder als Semiadmin (siehe unten); der Dienst speichert
selbst keine Passwörter.

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
| `JELLYFIN_API_KEY` | leer | Jellyfin-API-Schlüssel für Semiadmins; leer schaltet die Funktion aus. Gehört in `.env`, nicht in `docker-compose.yml` |
| `SETTINGS_FILE` | `/data/settings.json` | Pfad der Settings-Datei mit den Semiadmin-Zuordnungen |

## Semiadmins

Ein Semiadmin ist ein gewöhnliches Jellyfin-Konto, das bestimmte andere
Benutzer aktivieren und deaktivieren darf — typischerweise ein Elternteil für
die Konten der eigenen Kinder. Er meldet sich mit seinem normalen
Jellyfin-Konto an und sieht nur die ihm zugewiesenen Benutzer mit einem
Aktiv/Aus-Schalter; Bibliotheken kann er nicht ändern.

Einrichten:

1. Im Jellyfin-Dashboard unter *API-Schlüssel* einen Schlüssel anlegen.
2. Ihn in eine Datei `.env` neben `docker-compose.yml` schreiben:
   `JELLYFIN_API_KEY=<schlüssel>` — ohne Anführungszeichen und Leerzeichen.
3. Neu starten (`docker compose up -d`). Steht im Log
   „JELLYFIN_API_KEY konnte nicht geprüft werden: …", ist der Schlüssel
   falsch oder Jellyfin war beim Start nicht erreichbar.
4. Als Administrator die Detailseite des künftigen Semiadmins öffnen, im
   Abschnitt „Semiadmin" den Haken setzen, die Benutzer wählen, speichern.

Die Zuordnung liegt in `SETTINGS_FILE` (im Container `/data/settings.json`,
benanntes Volume `jellyfin-enabler-data`) und übersteht Neustarts. Wer statt
des benannten Volumes einen Bind-Mount nutzt, muss das Host-Verzeichnis dem
Benutzer 65532 geben (`sudo chown 65532:65532 ./data`), unter dem der Dienst
im Container läuft — sonst scheitert jedes Speichern mit „Einstellungen
konnten nicht gespeichert werden."

Ohne `JELLYFIN_API_KEY` ist die Funktion aus; eine vorhandene Zuordnung bleibt
in der Datei und wirkt wieder, sobald der Schlüssel gesetzt ist. Jede
Schaltung eines Semiadmins steht im Log des Dienstes
(`semiadmin schaltet: von=… ziel=… disabled=…`), weil Jellyfins
Aktivitätslog bei Aufrufen mit API-Schlüssel keinen Benutzer nennt.

## Entwicklung

```bash
make check      # gofmt, go vet, Tests
make css        # Tailwind im Container übersetzen
make run        # lokal starten, JELLYFIN_URL=… und SETTINGS_FILE=… überschreibbar
make start      # Container bauen, starten und die UI öffnen
```

Design, Makefile und Docker-Aufbau folgen dem Schwesterprojekt
`yt-dl-web-client`.

## Was der Dienst bewusst nicht kann

Benutzer anlegen oder löschen, Passwörter ändern, Sitzungen beenden. Dafür
gibt es das Jellyfin-Dashboard. Semiadmins können keine Bibliotheken ändern;
Gruppen und Zeitpläne gibt es nicht, und verwaiste Einträge gelöschter
Benutzer werden nicht automatisch aufgeräumt.

Wird ein bereits deaktivierter Benutzer in Jellyfin nachträglich zum
Administrator gemacht, lässt er sich über diese Oberfläche nicht mehr
bearbeiten — der Aktiv-Schalter ist dann gesperrt und steht auf "Aus",
weshalb Jellyfin jedes Speichern ablehnt. Auflösen lässt sich das im
Jellyfin-Dashboard.

## Sicherheitshinweis

Wer die Oberfläche erreicht und ein Admin-Passwort hat, kann Zugänge
abschalten. Der Dienst gehört ins Heimnetz, hinter Reverse Proxy und TLS —
nicht ungeschützt ins Internet.

`JELLYFIN_API_KEY` ist ein vollwertiger Admin-Zugang zu Jellyfin. Er gehört
nur in `.env` (per `.gitignore` ausgeschlossen), nicht in
`docker-compose.yml` und in kein Repository. Der Dienst loggt ihn nie und
benutzt ihn ausschließlich für die Semiadmin-Ansicht und das Schalten. Ist er
abgeflossen: im Jellyfin-Dashboard widerrufen und einen neuen anlegen.
