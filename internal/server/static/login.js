(function () {
  document.getElementById("error").hidden = !new URLSearchParams(window.location.search).has("error");
})();
