// Applies the dark theme before the page draws when the viewer chose it
// (src/lib/theme.ts), so that a dark page never flashes white; light is the
// default. A file rather than inline, as the console's
// Content-Security-Policy allows no inline script.
(function () {
  var dark = false;
  try {
    dark = localStorage.getItem("changeport-theme") === "dark";
  } catch (e) {}
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
})();
