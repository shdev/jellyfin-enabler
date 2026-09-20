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
