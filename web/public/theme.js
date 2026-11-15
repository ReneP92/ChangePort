// Applies the theme the viewer chose (src/lib/theme.ts), or their system's,
// before the page draws, so that a dark page never flashes white. A file
// rather than inline, as the console's Content-Security-Policy allows no
// inline script.
(function () {
  var dark = false;
  try {
    var chosen = localStorage.getItem("changeport-theme");
    dark = chosen === "dark" || (chosen !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  } catch (e) {
    dark = matchMedia("(prefers-color-scheme: dark)").matches;
  }
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
})();
