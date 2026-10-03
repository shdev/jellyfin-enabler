// Kurze Rückmeldung am unteren Bildschirmrand. art ist "ok", "warn" oder "err".
function toast(text, art = "ok") {
  const region = document.getElementById("toast-region");
  if (!region) return;
  const el = document.createElement("div");
  el.className = `toast toast-${art}`;
  el.innerHTML = '<span class="toast-dot"></span>';
  el.appendChild(document.createTextNode(text));
  region.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// Schickt einen Teil der Seite per PUT als JSON. Liefert Status und Rumpf;
// wirft nur, wenn der Dienst gar nicht erreichbar ist.
async function senden(pfad, rumpf) {
  const res = await fetch(pfad, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(rumpf),
  });
  let daten = {};
  try {
    daten = await res.json();
  } catch (err) {
    // Kein JSON (z. B. eine Fehlerseite des Reverse Proxys): dann greift
    // beim Aufrufer die Rückfallmeldung.
  }
  return { ok: res.ok, status: res.status, daten };
}

// Sammelt die Änderungen auf der Benutzerseite und schickt sie mit einem
// Klick — statt bei jedem Häkchen einen Request abzusetzen. Zugriff und
// Semiadmin-Zuordnung sind getrennte Endpunkte; gesendet wird nur der Teil,
// der sich geändert hat.
(function () {
  const form = document.getElementById("user-form");
  if (!form) return;

  const saveBtn = document.getElementById("save-btn");
  const hint = document.getElementById("save-hint");
  const allBox = document.getElementById("all-folders");
  const enabledOn = document.getElementById("enabled-on");
  const enabledOff = document.getElementById("enabled-off");
  // Fehlt bei Administratoren und ohne API-Key.
  const semiBox = document.getElementById("semiadmin-box");
  const folderBoxes = () =>
    Array.from(document.querySelectorAll("#libraries input[type=checkbox]"));
  const targetBoxes = () =>
    Array.from(document.querySelectorAll("#targets input[type=checkbox]"));
  const ausgewaehlt = (boxen) =>
    boxen
      .filter((b) => b.checked)
      .map((b) => b.value)
      .sort();

  function lesenZugriff() {
    return {
      disabled: enabledOn ? !enabledOn.checked : false,
      allFolders: allBox.checked,
      folders: ausgewaehlt(folderBoxes()),
    };
  }

  function lesenZuordnung() {
    if (!semiBox) return null;
    return { semiadmin: semiBox.checked, users: ausgewaehlt(targetBoxes()) };
  }

  // Die Stände, die der Server zuletzt bestätigt hat.
  let zugriff = lesenZugriff();
  let zuordnung = lesenZuordnung();

  function mengenAbstand(a, b) {
    const mengeA = new Set(a);
    const mengeB = new Set(b);
    let n = 0;
    for (const id of mengeA) if (!mengeB.has(id)) n++;
    for (const id of mengeB) if (!mengeA.has(id)) n++;
    return n;
  }

  function unterschiedeZugriff(a, b) {
    let n = 0;
    if (a.disabled !== b.disabled) n++;
    if (a.allFolders !== b.allFolders) n++;
    return n + mengenAbstand(a.folders, b.folders);
  }

  function unterschiedeZuordnung(a, b) {
    if (!a || !b) return 0;
    let n = a.semiadmin !== b.semiadmin ? 1 : 0;
    // Ohne Haken ignoriert der Server die Auswahl; dann zählt sie nicht.
    if (b.semiadmin) n += mengenAbstand(a.users, b.users);
    return n;
  }

  // Setzt die Felder auf die bestätigten Stände.
  function anwenden() {
    if (enabledOn) enabledOn.checked = !zugriff.disabled;
    if (enabledOff) enabledOff.checked = zugriff.disabled;
    allBox.checked = zugriff.allFolders;
    const freigegeben = new Set(zugriff.folders);
    folderBoxes().forEach((b) => {
      b.checked = freigegeben.has(b.value);
    });
    if (semiBox && zuordnung) {
      semiBox.checked = zuordnung.semiadmin;
      const ziele = new Set(zuordnung.users);
      targetBoxes().forEach((b) => {
        b.checked = ziele.has(b.value);
      });
    }
  }

  function aktualisieren() {
    // Die Einzelauswahl bleibt sichtbar, ist aber wirkungslos, solange
    // "Alle Bibliotheken" gesetzt ist.
    folderBoxes().forEach((b) => {
      b.disabled = allBox.checked;
    });
    // Die Chips sind nur wählbar, solange der Semiadmin-Haken gesetzt ist.
    if (semiBox) {
      targetBoxes().forEach((b) => {
        b.disabled = !semiBox.checked;
      });
    }
    const anzahl =
      unterschiedeZugriff(zugriff, lesenZugriff()) +
      unterschiedeZuordnung(zuordnung, lesenZuordnung());
    saveBtn.disabled = anzahl === 0;
    saveBtn.textContent = anzahl === 0 ? "Speichern" : `Speichern (${anzahl})`;
    hint.textContent =
      anzahl === 0
        ? "Keine Änderungen"
        : anzahl === 1
          ? "1 Änderung"
          : `${anzahl} Änderungen`;
  }

  // Zurück auf den bestätigten Stand, statt eine Änderung vorzutäuschen. Ein
  // bereits gespeicherter Teil bleibt gespeichert.
  function fehlschlag(text) {
    anwenden();
    aktualisieren();
    toast(text, "err");
  }

  form.addEventListener("change", aktualisieren);

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    const id = form.dataset.userId;
    try {
      if (unterschiedeZugriff(zugriff, lesenZugriff()) > 0) {
        const r = await senden(`/api/users/${id}/access`, lesenZugriff());
        if (r.status === 401) {
          window.location.href = "/login";
          return;
        }
        if (!r.ok) {
          fehlschlag(r.daten.error || "Speichern fehlgeschlagen.");
          return;
        }
        zugriff = {
          disabled: r.daten.disabled,
          allFolders: r.daten.allFolders,
          folders: (r.daten.folders || []).slice().sort(),
        };
      }
      if (unterschiedeZuordnung(zuordnung, lesenZuordnung()) > 0) {
        const r = await senden(`/api/users/${id}/delegation`, lesenZuordnung());
        if (r.status === 401) {
          window.location.href = "/login";
          return;
        }
        if (!r.ok) {
          fehlschlag(r.daten.error || "Speichern fehlgeschlagen.");
          return;
        }
        zuordnung = {
          semiadmin: r.daten.semiadmin,
          users: (r.daten.users || []).slice().sort(),
        };
      }
      anwenden();
      aktualisieren();
      toast("Gespeichert.", "ok");
    } catch (err) {
      fehlschlag("Der Dienst ist nicht erreichbar.");
    }
  });

  aktualisieren();
})();

// Semiadmin-Ansicht: jeder Schalter speichert sofort. Bei einem Fehler
// springt er auf den letzten Stand zurück, ein Toast zeigt die Meldung.
(function () {
  const liste = document.getElementById("controlled");
  if (!liste) return;

  liste.addEventListener("change", async (ev) => {
    const karte = ev.target.closest("[data-user-id]");
    if (!karte) return;
    const id = karte.dataset.userId;
    const an = document.getElementById(`on-${id}`);
    const aus = document.getElementById(`off-${id}`);
    const disabled = aus.checked;
    const zurueck = () => {
      an.checked = disabled;
      aus.checked = !disabled;
    };
    an.disabled = true;
    aus.disabled = true;
    try {
      const r = await senden(
        `/api/controlled/${encodeURIComponent(id)}/active`,
        { disabled },
      );
      if (r.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!r.ok) {
        zurueck();
        toast(r.daten.error || "Schalten fehlgeschlagen.", "err");
        return;
      }
      an.checked = !r.daten.disabled;
      aus.checked = r.daten.disabled;
      toast(r.daten.disabled ? "Deaktiviert." : "Aktiviert.", "ok");
    } catch (err) {
      zurueck();
      toast("Der Dienst ist nicht erreichbar.", "err");
    } finally {
      an.disabled = false;
      aus.disabled = false;
    }
  });
})();
