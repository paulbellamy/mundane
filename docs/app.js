/* Progressive enhancement only. The page is fully readable with JS off. */
(function () {
  "use strict";
  var root = document.documentElement;

  // --- theme toggle ---
  function setTheme(dark) {
    root.classList.toggle("dark", dark);
    try { localStorage.setItem("theme", dark ? "dark" : "light"); } catch (e) {}
  }
  document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      setTheme(!root.classList.contains("dark"));
    });
  });

  // --- mobile nav ---
  var navBtn = document.querySelector("[data-nav-toggle]");
  var nav = document.querySelector("[data-nav]");
  if (navBtn && nav) {
    navBtn.addEventListener("click", function () { nav.classList.toggle("hidden"); });
    nav.querySelectorAll("a").forEach(function (a) {
      a.addEventListener("click", function () { nav.classList.add("hidden"); });
    });
  }

  // --- code tabs ---
  document.querySelectorAll("[data-tabs]").forEach(function (group) {
    var tabs = group.querySelectorAll("[data-tab]");
    var panels = group.querySelectorAll("[data-panel]");
    function activate(name) {
      tabs.forEach(function (t) {
        t.setAttribute("aria-selected", t.getAttribute("data-tab") === name ? "true" : "false");
      });
      panels.forEach(function (p) {
        p.classList.toggle("hidden", p.getAttribute("data-panel") !== name);
      });
    }
    tabs.forEach(function (t) {
      t.addEventListener("click", function () { activate(t.getAttribute("data-tab")); });
    });
    if (tabs.length) activate(tabs[0].getAttribute("data-tab"));
  });

  // --- copy buttons ---
  document.querySelectorAll("[data-copy]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var text = btn.getAttribute("data-copy-text") || "";
      if (!text || !navigator.clipboard) return;
      navigator.clipboard.writeText(text).then(function () {
        var label = btn.getAttribute("data-label") || btn.textContent;
        btn.textContent = "copied";
        setTimeout(function () { btn.textContent = label; }, 1200);
      }).catch(function () {});
    });
  });

  // --- scroll-spy ---
  var links = Array.prototype.slice.call(document.querySelectorAll('[data-spy] a[href^="#"]'));
  var map = {};
  links.forEach(function (l) {
    var id = l.getAttribute("href").slice(1);
    var sec = document.getElementById(id);
    if (sec) map[id] = l;
  });
  if ("IntersectionObserver" in window && Object.keys(map).length) {
    var obs = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) return;
        links.forEach(function (l) { l.removeAttribute("data-active"); });
        var l = map[e.target.id];
        if (l) l.setAttribute("data-active", "");
      });
    }, { rootMargin: "-45% 0px -50% 0px", threshold: 0 });
    Object.keys(map).forEach(function (id) { obs.observe(document.getElementById(id)); });
  }
})();
