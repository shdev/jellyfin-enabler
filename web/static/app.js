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

// Sammelt die Änderungen auf der Benutzerseite und schickt sie mit einem
// Klick — statt bei jedem Häkchen einen Request abzusetzen.
(function () {
  const form = document.getElementById("user-form");
  if (!form) return;

  const saveBtn = document.getElementById("save-btn");
  const hint = document.getElementById("save-hint");
  const allBox = document.getElementById("all-folders");
  const enabledOn = document.getElementById("enabled-on");
  const enabledOff = document.getElementById("enabled-off");
  const folderBoxes = () =>
    Array.from(document.querySelectorAll("#libraries input[type=checkbox]"));

  function lesen() {
    return {
      disabled: enabledOn ? !enabledOn.checked : false,
      allFolders: allBox.checked,
      folders: folderBoxes()
        .filter((b) => b.checked)
        .map((b) => b.value)
        .sort(),
    };
  }

  // Der Stand, den der Server zuletzt bestätigt hat.
  let bestaetigt = lesen();

  function unterschiede(a, b) {
    let n = 0;
    if (a.disabled !== b.disabled) n++;
    if (a.allFolders !== b.allFolders) n++;
    const mengeA = new Set(a.folders);
    const mengeB = new Set(b.folders);
    for (const id of mengeA) if (!mengeB.has(id)) n++;
    for (const id of mengeB) if (!mengeA.has(id)) n++;
    return n;
  }

  function anwenden(stand) {
    if (enabledOn) enabledOn.checked = !stand.disabled;
    if (enabledOff) enabledOff.checked = stand.disabled;
    allBox.checked = stand.allFolders;
    const gesetzt = new Set(stand.folders);
    folderBoxes().forEach((b) => {
      b.checked = gesetzt.has(b.value);
    });
  }

  function aktualisieren() {
    // Die Einzelauswahl bleibt sichtbar, ist aber wirkungslos, solange
    // "Alle Bibliotheken" gesetzt ist.
    folderBoxes().forEach((b) => {
      b.disabled = allBox.checked;
    });
    const anzahl = unterschiede(bestaetigt, lesen());
    saveBtn.disabled = anzahl === 0;
    saveBtn.textContent = anzahl === 0 ? "Speichern" : `Speichern (${anzahl})`;
    hint.textContent =
      anzahl === 0
        ? "Keine Änderungen"
        : anzahl === 1
          ? "1 Änderung"
          : `${anzahl} Änderungen`;
  }

  form.addEventListener("change", aktualisieren);

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    try {
      const res = await fetch(`/api/users/${form.dataset.userId}/access`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(lesen()),
      });
      if (res.status === 401) {
        window.location.href = "/login";
        return;
      }
      const daten = await res.json();
      if (!res.ok) {
        // Zurück auf den Serverzustand, statt eine Änderung vorzutäuschen.
        anwenden(bestaetigt);
        aktualisieren();
        toast(daten.error || "Speichern fehlgeschlagen.", "err");
        return;
      }
      bestaetigt = {
        disabled: daten.disabled,
        allFolders: daten.allFolders,
        folders: (daten.folders || []).slice().sort(),
      };
      anwenden(bestaetigt);
      aktualisieren();
      toast("Gespeichert.", "ok");
    } catch (err) {
      anwenden(bestaetigt);
      aktualisieren();
      toast("Der Dienst ist nicht erreichbar.", "err");
    }
  });

  aktualisieren();
})();
