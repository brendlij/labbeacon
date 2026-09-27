"use strict";
const form = document.querySelector("#settings-form");
if (form) {
  const services = document.querySelector("#services");
  document.querySelector("#add-service").addEventListener("click", () => {
    services.append(document.querySelector("#service-template").content.cloneNode(true));
    services.lastElementChild.querySelector("input").focus();
  });
  services.addEventListener("click", event => {
    if (event.target.closest(".remove-service")) event.target.closest(".service").remove();
  });
  form.addEventListener("submit", () => {
    const checks = [...services.querySelectorAll(".service")].map(row => {
      const check = {};
      row.querySelectorAll("[data-field]").forEach(input => {
        check[input.dataset.field] = input.type === "checkbox" ? input.checked : input.type === "number" ? Number(input.value || 0) : input.value;
      });
      return check;
    });
    document.querySelector("#services-json").value = JSON.stringify(checks);
  });
}
if (document.querySelector("#live-status")) {
  setInterval(async () => {
    try {
      const response = await fetch("/", {cache: "no-store", credentials: "same-origin"});
      if (!response.ok) throw new Error("status");
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const status = page.querySelector("#live-status");
      if (!status) throw new Error("markup");
      document.querySelector("#live-status").replaceWith(status);
    } catch (_) {
      document.querySelector("#refresh-status").textContent = "Status nicht erreichbar. Angezeigte Werte können veraltet sein.";
    }
  }, 5000);
}
