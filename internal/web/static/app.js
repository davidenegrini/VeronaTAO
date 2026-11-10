// SPDX-License-Identifier: AGPL-3.0-or-later
// Comodità di interfaccia. Nessun calcolo clinico avviene qui: i valori
// definitivi sono sempre ricalcolati e verificati dal programma al salvataggio.
(function () {
  "use strict";

  // Totale in mg dello schema settimanale mentre si scrive (somma aritmetica).
  var drug = document.getElementById("drug");
  var total = document.getElementById("total");
  var inputs = document.querySelectorAll("input.q[name^=q]");
  function updateTotal() {
    if (!drug || !total || !inputs.length) return;
    var mg = parseInt(drug.selectedOptions[0].dataset.mg, 10);
    var sum = 0, complete = true;
    inputs.forEach(function (i) {
      if (i.value === "" || !/^\d+$/.test(i.value)) complete = false;
      else sum += parseInt(i.value, 10);
    });
    if (!complete || !mg) { total.textContent = ""; return; }
    var milli = sum * mg / 4;
    total.textContent = "Totale: " + (milli / 1000).toLocaleString("it-IT", { maximumFractionDigits: 3 }) + " mg";
  }
  inputs.forEach(function (i) { i.addEventListener("input", updateTotal); });
  if (drug) drug.addEventListener("change", updateTotal);

  // Con "solo INR" i campi del piano non servono e vengono disattivati.
  var onlyInr = document.querySelector("input[name=inr_only]");
  var plan = document.querySelector("fieldset.plan");
  function togglePlan() { if (onlyInr && plan) plan.disabled = onlyInr.checked; }
  if (onlyInr) { onlyInr.addEventListener("change", togglePlan); togglePlan(); }

  // Precompila il range con il valore di comodo dell'indicazione scelta:
  // resta modificabile e va verificato dal medico.
  var ind = document.getElementById("indication");
  if (ind) ind.addEventListener("change", function () {
    var o = ind.selectedOptions[0];
    ["min", "max", "target"].forEach(function (k) {
      var el = document.getElementById("inr_" + k);
      if (el && o && o.dataset[k] !== undefined) el.value = o.dataset[k] === "0,00" ? "" : o.dataset[k];
    });
  });

  // La direzione è richiesta solo se il farmaco è dichiarato interferente.
  var interfering = document.getElementById("interfering");
  var direction = document.getElementById("direction");
  function toggleDirection() {
    if (!interfering || !direction) return;
    direction.disabled = !interfering.checked;
    direction.required = interfering.checked;
    if (!interfering.checked) direction.value = "";
  }
  if (interfering) { interfering.addEventListener("change", toggleDirection); toggleDirection(); }
})();
